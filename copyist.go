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
	"path"
	"runtime"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
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

// MaxRecordingSize is the maximum size, in bytes, of a single recording in its
// text format.
var MaxRecordingSize = 1024 * 1024

// SessionInitCallback types a function that is invoked once per session for
// each driver, when in recording mode, in order to initialize the database to a
// clean, well-known state.
type SessionInitCallback func()

// sessionInit is called at the beginning of each new session, if not nil.
var sessionInit SessionInitCallback

// registered is the set of proxy drivers created via calls to Register, indexed
// by driver name.
var registered map[string]*proxyDriver

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
// "postgres"). Below is an example of how copyist.Register should be invoked.
//
//   copyist.Register("postgres")
//
// Note that Register can only be called once for a given driver; subsequent
// attempts will fail with an error. In addition, the same copyist driver must
// be used with playback as was was used during recording.
func Register(driverName string) {
	if registered == nil {
		registered = make(map[string]*proxyDriver)
	} else if _, ok := registered[driverName]; ok {
		panic(fmt.Errorf("Register called twice for driver %s", driverName))
	}

	copyistDriver := &proxyDriver{driverName: driverName}
	registered[driverName] = copyistDriver

	// sqlx uses a default list of driver names to determine how to represent
	// parameters in prepared queries. For example, postgres uses $1, mysql
	// uses ?, sqlserver uses @, and so on.  But since copyist defines a custom
	// driver name, sqlx falls back to the default ?, which won't work with some
	// databases. Register the copyist driver name with sqlx and tell it to use
	// the bind type of the underlying driver rather than the default ?.
	copyistDriverName := copyistDriverName(driverName)
	sqlx.BindDriver(copyistDriverName, sqlx.BindType(driverName))

	// Register the copyist driver with the `sql` package.
	sql.Register(copyistDriverName, copyistDriver)
}

// SetSessionInit sets the callback function that will be invoked at the
// beginning of each copyist session. This can be used to initialize the test
// database to a clean, well-known state.
//
// NOTE: The callback is only invoked in "recording" mode. There is no need to
// call it in "playback" mode, as the database is not actually accessed at that
// time.
func SetSessionInit(callback SessionInitCallback) {
	sessionInit = callback
}

// Open begins a recording or playback session, depending on the value of the
// "record" command-line flag. If recording, then all calls to registered
// drivers will be recorded and then saved in a copyist recording file that sits
// alongside the calling test file. If playing back, then the recording will
// be fetched from that recording file. Here is a typical calling pattern:
//
//   func init() {
//     copyist.Register("postgres")
//   }
//
//   func TestMyStuff(t *testing.T) {
//     defer copyist.Open(t).Close()
//     ...
//   }
//
// The call to Open will initiate a new recording session. The deferred call to
// Close will complete the recording session and write the recording to a file
// in the testdata/ directory, like:
//
//   mystuff_test.go
//   testdata/
//     mystuff_test.copyist
//
// Each test or sub-test that needs to be executed independently needs to record
// its own session.
func Open(t *testing.T) io.Closer {
	if registered == nil {
		panic(errors.New("Register was not called"))
	}

	// Get name of calling test file.
	fileName := findTestFile()

	// Construct the recording pathName name by locating the copyist recording
	// file in the testdata directory with the ".copyist" extension.
	dirName := path.Join(path.Dir(fileName), "testdata")
	fileName = path.Base(fileName[:len(fileName)-3]) + ".copyist"
	pathName := path.Join(dirName, fileName)

	// The recording name is the name of the test.
	recordingName := t.Name()

	return OpenNamed(pathName, recordingName)
}

// OpenNamed is a variant of Open which accepts a caller-specified pathName and
// recordingName rather than deriving default values for them. The given
// pathName will be used as the name of the output file containing the
// recordings rather than the default "_test.copyist" file in the testdata
// directory. The given recordingName will be used as the recording name in that
// file rather than using the testing.T.Name() value.
func OpenNamed(pathName, recordingName string) io.Closer {
	if registered == nil {
		panic("Register was not called")
	}

	// Start a new recording or playback session.
	currentSession = newSession(pathName, recordingName)

	// Return a closer that will close the session when called.
	return closer(func() error {
		currentSession.Close()
		currentSession = nil
		return nil
	})
}

// findTestFile searches the call stack, looking for the test that called
// copyist.Open. It searches up to N levels, looking for the last file that
// ends in "_test.go" and returns that filename.
func findTestFile() string {
	const levels = 10
	var lastTestFilename string
	for i := 0; i < levels; i++ {
		_, fileName, _, _ := runtime.Caller(2 + i)
		if strings.HasSuffix(fileName, "_test.go") {
			lastTestFilename = fileName
		}
	}
	if lastTestFilename != "" {
		return lastTestFilename
	}
	panic(fmt.Errorf("Open was not called directly or indirectly from a test file"))
}

// copyistDriverName constructs the copyist wrapper driver's name as a function
// of the wrapped driver's name.
func copyistDriverName(driverName string) string {
	return "copyist_" + driverName
}

// clearPooledConnections clears any pooled connection on all registered
// drivers, in order to ensure determinism. For more information, see the
// proxyDriver comment regarding connection pooling.
func clearPooledConnections() {
	for _, driver := range registered {
		driver.clearPooledConnection()
	}
}

// closer implements the io.Closer interface by invoking an arbitrary function
// when Close is called.
type closer func() error

// Close implements the io.Closer interface method.
func (c closer) Close() error {
	return c()
}
