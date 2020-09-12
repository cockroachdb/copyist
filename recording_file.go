// Copyright 2020 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package copyist

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"hash"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
)

// hashValue is an MD5 hash type (16 bytes).
type hashValue [md5.Size]byte

// recordingFile is the in-memory representation for a copyist recording file.
// recordingFile parses the file and stores its contents in data structures
// that make it convenient to get existing recordings, add new recordings, or
// write all buffered recordings to disk.
//
// The recording file is in the following format:
//
//   1=DriverOpen	1:nil
//   2=ConnPrepare	2:"SELECT name FROM customers WHERE id=$1"	1:nil
//   3=StmtNumInput	3:1
//   4=StmtQuery	1:nil
//   5=RowsColumns	9:["name"]
//   6=RowsNext	11:[2:"Andy"]	1:nil
//   7=RowsNext	11:[]	7:EOF
//
//   "github.com/cockroachdb/copyist/pqtest_test.TestQuery"=1,2,3,4,5,6,7
//
// The first section is a numbered list of tab-delimited copyist record
// declaration. Each record declaration represents a call to a driver method,
// and contains the method name and arguments pertaining to that method. Record
// declaration arguments are in a format documented by the formatValueWithType
// method, which specifies a string serialization of a value and its type.
//
// The second section is a mapping from a test recording name to the list of
// record numbers from the first section that make up that recording. It is
// common for multiple recording declarations to share one or more records,
// since driver calls are often quite redundant across tests.
type recordingFile struct {
	// pathName is the location of the copyist recording file (can be relative
	// or absolute).
	pathName string

	// recordDecls is a map of the parsed record declarations in the recording
	// file, keyed by the number of each declaration. The map value is the
	// string to the right of the equal sign (e.g. "Driver Open  1:nil").
	recordDecls map[int]string

	// recordingDecls is a map of the recording declarations in the recording
	// file, keyed by the recording name. The map value is the string to the
	// right of the equal sign (e.g. "1,2,3").
	recordingDecls map[string]string

	// addRecordings tracks any recordings added via calls to AddRecording.
	// Recordings are keyed by recording name. These are accumulated here until
	// WriteRecordingFile is called.
	addRecordings map[string]recording

	// md5Hasher is a reusable MD5 hasher.
	md5Hasher hash.Hash

	// scratch is a reusable bytes buffer.
	scratch bytes.Buffer
}

// newRecordingFile creates a new recordingFile data structure. Parse can be
// called to add recordings from an existing file, or AddRecording to add new
// recordings.
func newRecordingFile(pathName string) *recordingFile {
	return &recordingFile{pathName: pathName, md5Hasher: md5.New()}
}

// GetRecording returns the recording from the copyist recording file having the
// given name. If no such recording exists, then GetRecording returns nil.
func (f *recordingFile) GetRecording(recordingName string) recording {
	recordingDecl, ok := f.recordingDecls[recordingName]
	if !ok {
		return nil
	}

	// Found recording, now fully instantiate a recording object from the
	// recording declaration string.
	nums := f.parseRecordingDecl(recordingDecl)
	recording := make(recording, len(nums))
	for i := range nums {
		// Parse each record.
		rec := f.parseRecord(nums[i])
		recording[i] = rec
	}

	return recording
}

// AddRecording adds a new recording to the in-memory file, having the given
// name. Once WriteRecordingFile is called, added recordings will override any
// existing recordings and be written to disk.
func (f *recordingFile) AddRecording(recordingName string, newRecording recording) {
	if f.addRecordings == nil {
		f.addRecordings = make(map[string]recording)
	}
	f.addRecordings[recordingName] = newRecording
}

// WriteRecordingFile writes all recordings to the recording file in the copyist
// recording file format. All recordings buffered in memory will be written,
// with any recordings added by AddRecording overriding existing recordings.
// Only record declarations that are used by the written set of recordings will
// be written to disk.
func (f *recordingFile) WriteRecordingFile() {
	// Accumulate records and recordings that need to be written to disk.
	outRecordDecls := make([]string, 0, len(f.recordingDecls)+len(f.addRecordings))
	outRecordingDecls := make(map[string]string)
	hashToNumMap := make(map[hashValue]int)

	// addRecordDecl ensures that only unique record declarations are added to
	// outRecordDecls. It returns the unique record number assigned to the
	// given record declaration.
	addRecordDecl := func(recordDecl string) int {
		if len(recordDecl) > MaxRecordingSize {
			panic(errors.New("recording exceeds copyist.MaxRecordingSize and cannot be written"))
		}

		hashVal := f.hashStr(recordDecl)
		num, ok := hashToNumMap[hashVal]
		if !ok {
			num = len(outRecordDecls)
			hashToNumMap[hashVal] = num
			outRecordDecls = append(outRecordDecls, recordDecl)
		}
		return num
	}

	// formatRecording constructs a comma-delimited list of record numbers.
	formatRecording := func(recordNums []int) string {
		f.scratch.Reset()
		for i, num := range recordNums {
			if i != 0 {
				f.scratch.WriteByte(',')
			}
			f.scratch.WriteString(strconv.Itoa(num + 1))
		}
		return f.scratch.String()
	}

	// Add set of existing recording and record declarations to the output
	// data structures, as long as they were not overridden by added recordings.
	for recordingName, recordingDecl := range f.recordingDecls {
		// Skip past recording declarations that are being replaced.
		if _, ok := f.addRecordings[recordingName]; ok {
			continue
		}

		// Add all record declarations used by this recording declaration.
		// Record declarations may be renumbered, so rebuild the recording
		// declaration to reflect the new numbers.
		oldRecordNums := f.parseRecordingDecl(recordingDecl)
		newRecordNums := make([]int, len(oldRecordNums))
		for i, num := range oldRecordNums {
			recordDecl, ok := f.recordDecls[num]
			if !ok {
				panic(fmt.Errorf("record with number %d must exist", num))
			}
			newRecordNums[i] = addRecordDecl(recordDecl)
		}

		// Create new recording declaration represented as a string.
		outRecordingDecls[recordingName] = formatRecording(newRecordNums)
	}

	// Add set of new recording and record declarations to the output data
	// structures.
	for recordingName, recording := range f.addRecordings {
		newRecordNums := make([]int, len(recording))
		for i, record := range recording {
			newRecordNums[i] = addRecordDecl(f.formatRecord(record))
		}
		outRecordingDecls[recordingName] = formatRecording(newRecordNums)
	}

	// Write the record declarations to the buffer.
	f.scratch.Reset()
	for num, recordDecl := range outRecordDecls {
		f.scratch.WriteString(strconv.Itoa(num + 1))
		f.scratch.WriteByte('=')
		f.scratch.WriteString(recordDecl)
		f.scratch.WriteByte('\n')
	}

	// Write the recording declarations to the buffer.
	f.scratch.WriteByte('\n')
	for recordingName, recordingDecl := range outRecordingDecls {
		f.scratch.WriteString(strconv.Quote(recordingName))
		f.scratch.WriteByte('=')
		f.scratch.WriteString(recordingDecl)
		f.scratch.WriteByte('\n')
	}

	// Ensure directory exists.
	dirName := path.Dir(f.pathName)
	if _, err := os.Stat(dirName); os.IsNotExist(err) {
		if err := os.MkdirAll(dirName, 0777); err != nil {
			panic(err)
		}
	}

	// Write the bytes to disk.
	if err := ioutil.WriteFile(f.pathName, f.scratch.Bytes(), 0666); err != nil {
		panic(err)
	}
}

// Parse reads the copyist recording file and extracts recording and record
// declarations from it, and stores them in in-memory data structures for
// convenient and performant access.
func (f *recordingFile) Parse() error {
	file, err := os.Open(f.pathName)
	if err != nil {
		return fmt.Errorf("error opening copyist recording file: %v", err)
	}
	defer file.Close()

	recordDecls := make(map[int]string)
	recordingDecls := make(map[string]string)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, MaxRecordingSize)
	for scanner.Scan() {
		text := scanner.Text()
		if len(text) == 0 {
			continue
		}

		if text[0] != '"' {
			// Split the line on the first equal sign:
			//   1=DriverOpen 3:nil
			index := strings.Index(text, "=")
			if index == -1 {
				return fmt.Errorf("expected equals: %s", text)
			}

			recordNum, err := strconv.Atoi(text[:index])
			if err != nil {
				return fmt.Errorf("expected record number: %s", text)
			}

			recordDecls[recordNum-1] = text[index+1:]
		} else {
			// Split the line on the last equal sign:
			//   "some:name":1,2,3,4
			index := strings.LastIndex(text, "=")
			if index == -1 {
				return fmt.Errorf("expected equals: %s", text)
			}
			recordingName, err := strconv.Unquote(text[:index])
			if err != nil {
				return err
			}
			recordingDecl := text[index+1:]
			recordingDecls[recordingName] = recordingDecl
		}
	}

	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			err = errors.New("recording exceeds copyist.MaxRecordingSize and cannot be read")
		}
		return err
	}

	f.recordDecls = recordDecls
	f.recordingDecls = recordingDecls
	return nil
}

// parseRecordingDecl parses a recording declaration value in a format similar
// to "1,2,3,4" and returns the resulting list of 0-based record numbers.
func (f *recordingFile) parseRecordingDecl(decl string) []int {
	numStrs := splitString(decl, ",")
	nums := make([]int, len(numStrs))
	for i := range numStrs {
		num, err := strconv.Atoi(numStrs[i])
		if err != nil {
			panic(err)
		}

		// Convert from 1-based record number to 0-based number.
		nums[i] = num - 1
	}
	return nums
}

// formatRecord returns the given copyist record as a string in a format like:
//
//   ConnPrepare 2:"SELECT COUNT(*) FROM customers"	1:nil
//
func (f *recordingFile) formatRecord(record *record) string {
	f.scratch.Reset()
	f.scratch.WriteString(record.Typ.String())
	for _, arg := range record.Args {
		f.scratch.WriteByte('\t')
		f.scratch.WriteString(formatValueWithType(arg))
	}
	return f.scratch.String()
}

// parseRecord instantiates the copyist record declaration identified by the
// given number in the copyist recording file.
func (f *recordingFile) parseRecord(recordNum int) *record {
	r, ok := f.recordDecls[recordNum]
	if !ok {
		panic(fmt.Errorf("record with number %d must exist", recordNum))
	}

	// Record fields are separated by tabs, with the first field being the name
	// of the driver method.
	fields := splitString(r, "\t")
	recType, ok := strToRecType[fields[0]]
	if !ok {
		panic(fmt.Errorf("record type %v is not recognized", fields[0]))
	}

	// Remaining fields are record arguments in "<dataType>:<formattedValue>"
	// format.
	rec := &record{Typ: recType}
	for i := 1; i < len(fields); i++ {
		val, err := parseValueWithType(fields[i])
		if err != nil {
			panic(fmt.Errorf("error parsing %s: %v", fields[i], err))
		}
		rec.Args = append(rec.Args, val)
	}
	return rec
}

// hashStr returns the MD5 hash of the given string.
func (f *recordingFile) hashStr(s string) hashValue {
	f.scratch.Reset()
	f.scratch.WriteString(s)
	f.md5Hasher.Reset()
	f.md5Hasher.Write(f.scratch.Bytes())

	var val hashValue
	copy(val[:], f.md5Hasher.Sum(nil))
	return val
}
