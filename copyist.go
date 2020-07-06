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
	"runtime"
	"strings"
)

// record instructs copyist to record all calls to the registered driver, if
// true. Otherwise, it plays back previously recorded calls.
var record = flag.Bool("record", true, "record sql database accesses")

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
			*record = false
		}
		visitedRecording = true
	}
	return *record
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

// recordingMap maps from driverName + funcName to the recording made for that
// combination.
var recordingMap = make(map[string]Recording)

// AddRecording is called by the generated code to enter all the recordings into
// the map as part of initialization. Those recordings can then be accessed for
// playback.
func AddRecording(recordingName string, recording Recording) {
	recordingMap[recordingName] = recording
}

// Register constructs a proxy driver that wraps the "real" driver of the given
// name. Depending on the value of the "record" command-line flag, the
// constructed proxy will either record calls to the wrapped driver, or else
// play back calls that were previously recorded. Register must be called before
// Open can be called, typically in a TestMain method or equivalent.
//
// Register method takes the name of the SQL driver to be wrapped (e.g.
// "postgres"). The resetDB function (if defined) resets the database to a
// clean, well-known state. It is only called in "recording" mode, each time
// that copyist.Open is called by a test. There is no need to call it in
// "playback" mode, as the database is not actually accessed at that time.
//
// Below is an example of how copyist.Register should be invoked.
//
//   err := copyist.Register("postgres", resetDB)
//
// Note that Register can only be called once; subsequent attempts will fail
// with an error. In addition, the same driver must be used with playback as was
// was used during recording.
func Register(driverName string, resetDB ResetCallback) error {
	if registered != nil {
		return errors.New("Register cannot be called more than once")
	}

	// Get the "real" driver that will be wrapped. Unfortunately, the sql
	// package does not offer any good way to do this. Calling Open and then
	// getting the driver from the DB object is a hacky workaround.
	// TODO(andyk): any better way to do this? Calling Open will fail for
	// drivers that immediately open a connection rather than doing it lazily.
	db, err := sql.Open(driverName, "")
	if err != nil {
		return err
	}
	wrapped := db.Driver()
	db.Close()

	if IsRecording() {
		registered = &proxyDriver{resetDB: resetDB, wrapped: wrapped, driverName: driverName}
	} else {
		registered = &proxyDriver{driverName: driverName}
	}
	sql.Register(copyistDriverName(driverName), registered)

	return nil
}

// Open begins a recording or playback session, depending on the value of the
// "record" command-line flag. If recording, then all calls to the registered
// driver will be recorded and then saved as Go code in a generated file that
// sits alongside the calling test file. If playing back, then the recording
// will be fetched from that generated file. Here is a typical calling pattern:
//
//   func TestMain(m *testing.M) {
//     flag.Parse()
//     copyist.Register("postgres")
//     os.Exit(m.Run())
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
//   mystuff_copyist_test.go
//
// Each test (or sub-test) should record its own session so that they can be
// executed independently.
func Open() io.Closer {
	if registered == nil {
		panic("Register was not called")
	}

	// Get name and path of calling test function.
	fileName, funcName := findTestFileAndName()

	// Synthesize the recording name by prepending the driver name.
	recordingName := fmt.Sprintf("%s/%s", registered.driverName, funcName)

	if IsRecording() {
		// Construct the recording file name by prefixing the "_test" suffix
		// with "_copyist".
		fileName = fileName[:len(fileName)-8] + "_copyist_test.go"

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
		registered.recording = Recording{}
		registered.index = 0

		// Once the recording session has been closed, construct a new AddRecording
		// call and add it to the body of the init function.
		return closer(func() error {
			generateRecordingFile(registered.recording, recordingName, fileName)

			registered.recording = nil

			return nil
		})
	}

	recording, ok := recordingMap[recordingName]
	if !ok {
		panic(fmt.Sprintf("no recording exists with this name: %v", recordingName))
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
	panic(fmt.Sprintf("Open was not called directly or indirectly from a test file"))
}

// copyistDriverName constructs the copyist wrapper driver's name as a function
// of the wrapped driver's name.
func copyistDriverName(driverName string) string {
	return "copyist_" + driverName
}
