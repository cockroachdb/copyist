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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenWithoutRegister tests that copyist.Open panics if copyist.Register
// was never called.
func TestOpenWithoutRegister(t *testing.T) {
	require.False(t, IsOpen())
	require.PanicsWithError(t, "Register was not called", func() {
		Open(t)
	})
}

// TestMultipleRegisterCalls tests that copyist.Register is an error when called
// multiple times. Test that there is no error when calling with a different
// driver.
func TestMultipleRegisterCalls(t *testing.T) {
	Register("multiple-register-driver-1")
	require.PanicsWithError(t, "Register called twice for driver multiple-register-driver-1", func() {
		Register("multiple-register-driver-1")
	})

	// Should be no error.
	Register("multiple-register-driver-2")
}

// TestUnknownDriver tests that copyist.Driver.Open returns an error when an
// unknown driver name is passed to copyist.Register.
func TestUnknownDriver(t *testing.T) {
	// Force recording mode.
	*recordFlag = true
	visitedRecording = true

	registered = nil
	Register("unknown")
	Open(t)
	db, err := sql.Open("copyist_unknown", "")
	require.NoError(t, err)
	_, err = db.Query("SELECT 1")
	require.Error(t, err, `sql: unknown driver "unknown"`)
}

// TestRecordingNotFound tests that copyist panics when trying to playback a
// recording that does not exist.
func TestRecordingNotFound(t *testing.T) {
	// Enter playback mode.
	*recordFlag = false
	visitedRecording = true

	Register("postgres")
	Open(t)
	db, err := sql.Open("copyist_postgres", "")
	require.NoError(t, err)
	require.PanicsWithError(
		t,
		`no recording exists with this name: TestRecordingNotFound`,
		func() { db.Query("SELECT 1") },
	)
}

// TestCopyistEnvVar tests that copyist respects the COPYIST_RECORD environment
// variable.
func TestCopyistEnvVar(t *testing.T) {
	// Enter playback mode.
	require.NoError(t, os.Setenv("COPYIST_RECORD", "TRUE"))
	*recordFlag = false
	visitedRecording = false
	require.True(t, IsRecording())
}

// TestFindTestFile tests that copyist finds the top-level *_test.go file.
func TestFindTestFile(t *testing.T) {
	require.Equal(t, "copyist_test.go", filepath.Base(indirectFindTestFile()))
}

func ignorePanic(f func()) {
	defer func() {
		recover()
	}()
	f()
}
