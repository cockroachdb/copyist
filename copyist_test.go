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
	"github.com/stretchr/testify/require"
	"testing"
)

// TestOpenWithoutRegister tests that copyist.Open panics if copyist.Register
// was never called.
func TestOpenWithoutRegister(t *testing.T) {
	registered = nil
	require.PanicsWithError(t, "Register was not called", func() {
		Open()
	})
}

// TestMultipleRegisterCalls tests that copyist.Register panics when it is
// called multiple times.
func TestMultipleRegisterCalls(t *testing.T) {
	// Ignore any panic on first call in case another test has already
	// registered the postgres driver.
	registered = nil
	ignorePanic(func() { Register("postgres", nil) })
	require.PanicsWithError(t, "Register cannot be called more than once", func() {
		Register("postgres", nil)
	})
}

// TestUnknownDriver tests that copyist.Driver.Open returns an error when an
// unknown driver name is passed to copyist.Register.
func TestUnknownDriver(t *testing.T) {
	// Force recording mode.
	*record = true
	visitedRecording = true

	registered = nil
	Register("unknown", nil)
	Open()
	db, err := sql.Open("copyist_unknown", "")
	require.NoError(t, err)
	_, err = db.Query("SELECT 1")
	require.Error(t, err, `sql: unknown driver "unknown"`)
}

// TestRecordingNotFound tests that copyist panics when trying to playback a
// recording that does not exist.
func TestRecordingNotFound(t *testing.T) {
	// Enter playback mode.
	*record = false
	visitedRecording = true

	// Ignore any panic on first call in case another test has already
	// registered the postgres driver.
	registered = nil
	ignorePanic(func() { Register("postgres", nil) })
	require.PanicsWithError(
		t,
		`no recording exists with this name: postgres/github.com/cockroachdb/copyist.TestRecordingNotFound.func2`,
		func() { Open() },
	)
}

// TestTooManyCalls tests that copyist panics when trying to playback a
// mismatched recording.
func TestTooManyCalls(t *testing.T) {
	// Enter playback mode and create a mismatched recording.
	*record = false
	visitedRecording = true
	recordingMap["unknown/github.com/cockroachdb/copyist.TestTooManyCalls"] = Recording{}

	// Ignore any panic on first call in case another test has already
	// registered the postgres driver.
	registered = nil
	ignorePanic(func() { Register("unknown", nil) })
	Open()
	db, err := sql.Open("copyist_unknown", "")
	require.NoError(t, err)
	require.PanicsWithError(
		t,
		`too many calls to DriverOpen - regenerate recording`,
		func() { db.Query("SELECT 1") },
	)
}

func ignorePanic(f func()) {
	defer func() {
		recover()
	}()
	f()
}
