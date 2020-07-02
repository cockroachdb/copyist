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

//go:generate stringer -type=RecordType

package copyist

import (
	"database/sql/driver"
	"fmt"
)

// RecordType identifies the SQL driver method that was called during the
// recording process. It is stored as part of the Record struct, and is checked
// during playback.
type RecordType int32

// This is a list of the event types, which correspond 1:1 with SQL driver
// methods.
const (
	_ RecordType = iota
	DriverOpen
	ConnPrepare
	ConnBegin
	StmtNumInput
	StmtExec
	StmtQuery
	TxCommit
	TxRollback
	ResultLastInsertId
	ResultRowsAffected
	RowsColumns
	RowsNext
)

// RecordArgs is an untyped list of arguments and/or return values to/from a SQL
// driver method that was called during the recording process. It is stored as
// part of the Record struct that is stored, and is checked or returned during
// playback.
type RecordArgs []interface{}

// Record stores information for one SQL driver method that is called during the
// recording process. During playback, this record provides the information
// needed to verify the expected method is called, to check the right arguments
// are passed, and to provide the return value(s) from the method.
type Record struct {
	// Typ is the driver method that was called during the recording process.
	Typ RecordType

	// Args are driver method arguments and/or return values that are needed for
	// playback.
	Args RecordArgs
}

// proxyDriver records and plays back calls to driver.Driver methods.
type proxyDriver struct {
	// resetDB (if defined) resets the database to a clean, well-known state. It
	// is only called in "recording" mode, each time that copyist.Open is called
	// by a test.
	resetDB ResetCallback

	// wrapped is the underlying driver that is being "recorded". This is nil
	// if in playback mode.
	wrapped driver.Driver

	// driverName is the name of the wrapped driver.
	driverName string

	// recording stores the calls made to the driver so they can be played back
	// later.
	recording []Record

	// index is the current offset into the recording slice. It is used only
	// during playback mode.
	index int
}

// Open returns a new connection to the database.
// The name is a string in a driver-specific format.
//
// Open may return a cached connection (one previously
// closed), but doing so is unnecessary; the sql package
// maintains a pool of idle connections for efficient re-use.
//
// The returned connection is only used by one goroutine at a
// time.
func (d *proxyDriver) Open(name string) (driver.Conn, error) {
	if d.isRecording() {
		conn, err := d.wrapped.Open(name)
		d.recording = append(d.recording, Record{Typ: DriverOpen, Args: RecordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyConn{driver: d, conn: conn}, nil
	}

	record := d.verifyRecord(DriverOpen)
	err, _ := record.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyConn{driver: d}, nil
}

// isRecording is true if the driver is in recording mode, or false if in
// playback mode.
func (d *proxyDriver) isRecording() bool {
	return d.wrapped != nil
}

// verifyRecord returns one of the records in recording, failing with a nice
// error if no such record exists.
func (d *proxyDriver) verifyRecord(recordTyp RecordType) Record {
	if d.recording == nil {
		panic("copyist.Open was never called")
	}
	if d.index >= len(d.recording) {
		panic(fmt.Sprintf("too many calls to %s - regenerate recording", recordTyp.String()))
	}
	record := d.recording[d.index]
	if record.Typ != recordTyp {
		panic(fmt.Sprintf("unexpected call to %s - regenerate recording", recordTyp.String()))
	}
	d.index++
	return record
}

// verifyRecordWithStringArg returns one of the records in recording, failing
// with a nice error if no such record exists, or if its first argument does not
// match the given string.
func (d *proxyDriver) verifyRecordWithStringArg(recordTyp RecordType, arg string) Record {
	record := d.verifyRecord(recordTyp)
	if record.Args[0].(string) != arg {
		panic(fmt.Sprintf("mismatched argument to %s, expected %s, got %s - regenerate recording",
			recordTyp.String(), arg, record.Args[0].(string)))
	}
	return record
}
