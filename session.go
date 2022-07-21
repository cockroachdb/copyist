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
	"fmt"
	"os"
)

// session is state used during copyist recording and playback to track progress
// of any currently open session.
type session struct {
	// recording stores the calls made to registered drivers used in the current
	// sessions so that the calls can be played back later.
	recording recording

	// index is the current offset into the recording slice. It is used only
	// during playback mode.
	index int

	// recordingSource is the in-memory representation for the copyist recordingSource being read or
	// written by this session.
	recordingSource *recordingSource

	// recordingName is the name of the recording currently being made.
	recordingName string

	// isInit is set to true once this session has been initialized.
	isInit bool
}

// currentSession is a global instance of session that tracks state for the
// current copyist session. It is nil if no session is currently open.
var currentSession *session

// IsOpen is true if a recording or playback session is currently in progress.
// That is, Open or OpenNamed has been called, but Close has not yet been
// called. This is useful when some tests use copyist and some don't, and
// testing utility code wants to automatically determine whether to open a
// connection using the copyist driver or the "real" driver.
func IsOpen() bool {
	return currentSession != nil
}

// newSession creates a new recording or playback session. The session will
// read or write a new recording of the given name in the given source.
func newSession(source Source, recordingName string) *session {
	return &session{
		recording:       recording{},
		recordingSource: newRecordingSource(source),
		recordingName:   recordingName,
	}
}

// OnDriverOpen is called by the proxy drivers when their Open method is called
// by the golang `sql` package to open a new connection. OnDriverOpen performs
// initialization steps for the session and for the driver.
func (s *session) OnDriverOpen(driver *proxyDriver) {
	// If session has already been initialized, then no-op.
	if s.isInit {
		return
	}
	s.isInit = true

	if IsRecording() {
		// Invoke sessionInit callback for the driver, if defined. Only do this
		// when recording, to give the callback a chance to set the database in
		// a clean, well-known state.
		if sessionInit != nil {
			sessionInit()
		}
	} else {
		// Need to play back a recording file, so parse it now.
		if err := s.recordingSource.Parse(); err != nil && !os.IsNotExist(err) {
			panicf("error parsing recording file: %v", err)
		}

		// Set the list of records to play back for the current session.
		s.recording = s.recordingSource.GetRecording(s.recordingName)
		if s.recording == nil {
			panicf("no recording exists with this name: %v", s.recordingName)
		}
	}

	// Clear any connections left over from previous sessions so that they don't
	// cause non-deterministic behavior for this test.
	clearPooledConnections()
}

// AddRecord adds a record to the current recording.
func (s *session) AddRecord(rec *record) {
	s.recording = append(s.recording, rec)
}

// VerifyRecordWithStringArg returns one of the records in this session's
// recording, failing with a nice error if no such record exists, or if its
// first argument does not match the given string.
func (s *session) VerifyRecordWithStringArg(recordTyp recordType, arg string) *record {
	rec := s.VerifyRecord(recordTyp)
	if rec.Args[0].(string) != arg {
		panicf(
			"mismatched argument to %s, expected %s, got %s\n\n"+
				"Do you need to regenerate the recording with the -record flag?",
			recordTyp.String(), rec.Args[0].(string), arg)
	}
	return rec
}

// VerifyRecord returns one of the records in this session's recording, failing
// with a nice error if no such record exists.
func (s *session) VerifyRecord(recordTyp recordType) *record {
	if s.index >= len(s.recording) {
		panicf(
			"too many calls to %s\n\n"+
				"Do you need to regenerate the recording with the -record flag?", recordTyp.String())
	}
	rec := s.recording[s.index]
	if rec.Typ != recordTyp {
		panicf(
			"unexpected call to %s\n\n"+
				"Do you need to regenerate the recording with the -record flag?", recordTyp.String())
	}
	s.index++
	return rec
}

// Close ends this session, writing any recording file and clearing state.
func (s *session) Close() {
	// Only create a recording file if records exist.
	if IsRecording() && len(s.recording) != 0 {
		// If no recording file exists, or there is parse error, then ignore the
		// error and create a new file. Parse errors can happen when there's a
		// Git merge conflict, and it's convenient to just silently regenerate
		// the file.
		_ = s.recordingSource.Parse()

		// Add the recording to the in-memory file and then write the file to
		// disk.
		s.recordingSource.AddRecording(s.recordingName, s.recording)
		s.recordingSource.WriteRecording()
	}

	// Clear any connections pooled during the recording process so that they
	// don't leak or cause non-deterministic behavior for the next test.
	clearPooledConnections()
}

func panicf(format string, args ...interface{}) {
	panic(&sessionError{fmt.Errorf(format, args...)})
}

type sessionError struct {
	error
}
