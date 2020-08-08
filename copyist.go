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
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// recordFlag instructs copyist to record all calls to the registered driver, if
// true. Otherwise, it plays back previously recorded calls.
var recordFlag = flag.Bool("record", true, "record sql database accesses")

var visitedRecording bool

// IsRecording returns true if copyist is currently in recording mode.
func IsRecording() bool {
	// Determine whether the "record" flag was explicitly passed rather than
	// defaulted. This is painful and slow in Go, so do it just once.
	if !visitedRecording {
		found := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "record" {
				found = true
			}
		})
		if !found {
			// If the record flag was not explicitly specified, then next check
			// the value of the COPYIST_RECORD environment variable.
			if os.Getenv("COPYIST_RECORD") != "" {
				*recordFlag = true
			} else {
				*recordFlag = false
			}
		}
		visitedRecording = true
	}
	return *recordFlag
}

// ResetCallback types a function that is invoked during each call to
// copyist.Open, when in recording mode, in order to reset the database to a
// clean, well-known state.
type ResetCallback func()

// closer implements the io.Closer interface by invoking an arbitrary function
// when Close is called.
type closer func() error

// Close implements the io.Closer interface method.
func (c closer) Close() error {
	return c()
}

// registered is the proxy driver created during the registration process. It is
// nil if Register has not yet been called.
var registered *proxyDriver

// Register constructs a proxy driver that wraps the "real" driver of the given
// name. Depending on the value of the "record" command-line flag, the
// constructed proxy will either record calls to the wrapped driver, or else
// play back calls that were previously recorded. Register must be called before
// copyist.Open can be called, typically in an init() method. Note that the
// wrapped driver is lazily fetched from the `sql` package, so if a driver of
// that name does not exist, an error will not be raised until a connection is
// opened for the first time.
//
// The Register method takes the name of the SQL driver to be wrapped (e.g.
// "postgres"). The resetDB function (if defined) resets the database to a
// clean, well-known state. It is only called in "recording" mode, each time
// that copyist.Open is called by a test. There is no need to call it in
// "playback" mode, as the database is not actually accessed at that time.
//
// Below is an example of how copyist.Register should be invoked.
//
//   copyist.Register("postgres", resetDB)
//
// Note that Register can only be called once; subsequent attempts will fail
// with an error. In addition, the same driver must be used with playback as was
// was used during recording.
func Register(driverName string, resetDB ResetCallback) {
	if registered != nil {
		panic(errors.New("Register cannot be called more than once"))
	}

	registered = &proxyDriver{resetDB: resetDB, driverName: driverName}

	// Register the copyist driver with the `sql` package.
	sql.Register(copyistDriverName(driverName), registered)
}

// Open begins a recording or playback session, depending on the value of the
// "record" command-line flag. If recording, then all calls to the registered
// driver will be recorded and then saved in a copyist recording file that sits
// alongside the calling test file. If playing back, then the recording will
// be fetched from that recording file. Here is a typical calling pattern:
//
//   func init() {
//     copyist.Register("postgres", resetDB)
//   }
//
//   func TestMyStuff(t *testing.T) {
//     defer copyist.Open().Close()
//     ...
//   }
//
// The call to Open will initiate a new recording session. The deferred call to
// Close will complete the recording session and write the recording to a file
// alongside the test file, such as:
//
//   mystuff_test.go
//   mystuff_test_copyist.txt
//
// Each test (or sub-test) should record its own session so that they can be
// executed independently.
func Open() io.Closer {
	if registered == nil {
		panic(errors.New("Register was not called"))
	}

	// Get name and path of calling test function.
	fileName, funcName := findTestFileAndName()

	// Construct the recording file name by suffixing with "_copyist.txt".
	fileName = fileName[:len(fileName)-3] + "_copyist.txt"

	// The recording name is just the test function name.
	// TODO(andyk): Consider appending testing.T test name in order to support
	// subtests. This would require Open to take a testing.T parameter.
	recordingName := funcName

	return OpenNamed(fileName, recordingName)
}

// OpenNamed is a variant of Open which accepts a caller-specified fileName and
// recordingName rather than deriving default values for them. The given
// fileName will be used as the name of the output file containing the
// recordings rather than the default "_copyist_test.txt" file. The given
// recordingName will be used as the recording name in that file rather than
// calculating the default name of "<testFunctionName>".
func OpenNamed(fileName, recordingName string) io.Closer {
	if registered == nil {
		panic("Register was not called")
	}

	f := newRecordingFile(fileName)

	if IsRecording() {
		// Invoke resetDB callback, if defined.
		if registered.resetDB != nil {
			registered.resetDB()
		}

		// Clear any pooled connection in order to ensure determinism. For more
		// information, see the proxyDriver comment regarding connection
		// pooling. Call this after resetDB, in case developer is using copyist
		// during the reset process (they shouldn't, but better to behave better
		// if they do).
		registered.clearPooledConnection()

		// Reset recording (including any recording that occurred during the
		// database reset).
		registered.recording = recording{}
		registered.index = 0

		// Once the recording session has been closed, construct a new
		// AddRecording call and add it to the body of the init function.
		return closer(func() error {
			// If no recording file exists, or there is parse error, then ignore
			// the error and create a new file. Parse errors can happen when
			// there's a Git merge conflict, and it's convenient to just
			// silently regenerate the file.
			f.Parse()
			f.AddRecording(recordingName, registered.recording)
			f.WriteRecordingFile(fileName)
			registered.recording = nil
			return nil
		})
	}

	// If recording file exists, parse it now.
	if _, err := os.Stat(f.fileName); !os.IsNotExist(err) {
		err := f.Parse()
		if err != nil {
			panic(fmt.Errorf("error parsing recording file: %v", err))
		}
	}

	recording := f.GetRecording(recordingName)
	if recording == nil {
		panic(fmt.Errorf("no recording exists with this name: %v", recordingName))
	}

	// Clear any pooled connection in order to ensure determinism. For more
	// information, see the proxyDriver comment regarding connection pooling.
	registered.clearPooledConnection()

	// Reset the registered driver with the recording to play back.
	registered.recording = recording
	registered.index = 0

	// Close is a no-op for playback.
	return closer(func() error {
		registered.recording = nil
		registered.index = 0
		return nil
	})
}

// findTestFileAndName searches the call stack, looking for the test that called
// copyist.Open. Search up to N levels, looking for a file that ends in
// "_test.go" and extract the function name from it. Return both the filename
// and function name.
func findTestFileAndName() (fileName, funcName string) {
	const levels = 5
	for i := 0; i < levels; i++ {
		var pc uintptr
		pc, fileName, _, _ = runtime.Caller(2 + i)
		if strings.HasSuffix(fileName, "_test.go") {
			// Extract package name from calling function name.
			funcName = runtime.FuncForPC(pc).Name()
			return fileName, funcName
		}
	}
	panic(fmt.Errorf("Open was not called directly or indirectly from a test file"))
}

// copyistDriverName constructs the copyist wrapper driver's name as a function
// of the wrapped driver's name.
func copyistDriverName(driverName string) string {
	return "copyist_" + driverName
}
