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
	"context"
	"database/sql"
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

type Recording []*Record

// proxyDriver records and plays back calls to driver.Driver methods.
//
// proxyDriver and proxyConn work together to take over connection pooling from
// the `sql` package. Connection pooling at any layer above the copyist driver
// is problematic, because it introduces non-determinism into recording
// sessions. Depending on whether a connection already exists, Driver.Open may
// or may not be called, with different recordings produced in each case.
//
// copyist disables `sql` package connection pooling by always returning
// driver.ErrBadConn from the driver.SessionResetter.ResetSession method, and
// instead pooling the connection in proxyDriver. In effect, copyist has a
// simple connection pool of size 1. That "pool" is cleared when copyist.Open is
// called, in order to ensure determinism. In addition, the driver maintains a
// monotonically increasing sequence number that identifies the current copyist
// session. Each time copyist.Open is called, the session is incremented.
// Connections created by earlier sessions cannot be reused. This ensures that
// copyist sessions are deterministic with regards to connection pooling - each
// session starts fresh.
type proxyDriver struct {
	// Driver is the interface that must be implemented by a database
	// driver.
	//
	// Database drivers may implement DriverContext for access
	// to contexts and to parse the name only once for a pool of connections,
	// instead of once per connection.
	driver.Driver

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
	recording Recording

	// index is the current offset into the recording slice. It is used only
	// during playback mode.
	index int

	// pooled caches a copyist connection for reuse. For more information, see
	// the proxyDriver comment regarding connection pooling.
	pooled *proxyConn

	// session is a monotonically increasing sequence number that identifies the
	// current copyist session. For more information, see the proxyDriver
	// comment regarding connection pooling.
	session int
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
	// Reuse pooled connection, if available and matching.
	if conn := d.tryReuseConnection(name); conn != nil {
		return conn, nil
	}

	if IsRecording() {
		// Lazily get the wrapped driver.
		if d.wrapped == nil {
			// Open the database in order to get the sql.Driver object to wrap.
			db, err := sql.Open(d.driverName, name)
			if err != nil {
				return nil, err
			}
			d.wrapped = db.Driver()
			db.Close()
		}

		conn, err := d.wrapped.Open(name)
		d.recording = append(d.recording, &Record{Typ: DriverOpen, Args: RecordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyConn{driver: d, conn: conn, name: name, session: d.session}, nil
	}

	record := d.verifyRecord(DriverOpen)
	err, _ := record.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyConn{driver: d, name: name, session: d.session}, nil
}

// tryPoolConnection puts the given connection into the pool if:
//   1. There is no connection in the pool already.
//   2. The connection was created by the current copyist session, not by a
//      previous session. This check is necessary to ensure that connections are
//      always re-opened for each session.
//   3. ResetSession on the underlying connection succeeds (or if the underlying
//      connection is nil, or doesn't implement the driver.SessionResetter
//      interface).
func (d *proxyDriver) tryPoolConnection(c *proxyConn) bool {
	if d.pooled != nil {
		// Already another connection in the pool.
		return false
	}

	if d.session != c.session {
		// Connection was opened during a previous copyist session.
		return false
	}

	// Call ResetSession on the underlying connection, if it is implemented.
	if resetter, ok := c.conn.(driver.SessionResetter); ok {
		// TODO(andyk): Should we try to save and then use the context
		// passed to ResetSession?
		if resetter.ResetSession(context.Background()) != nil {
			// Failed to reset.
			return false
		}
	}

	// Pool the connection for reuse.
	c.driver.pooled = c
	return true
}

// tryReuseConnection returns the pooled connection if it exists and if its name
// matches the given name, or nil if not.
func (d *proxyDriver) tryReuseConnection(name string) *proxyConn {
	if d.pooled != nil && d.pooled.name == name {
		pooled := d.pooled
		d.pooled = nil
		return pooled
	}
	return nil
}

// clearPooledConnection closes and clears the pooled connection, if it exists.
// This also increments the session id, so that any connections with a lower
// session cannot be returned to the pool.
func (d *proxyDriver) clearPooledConnection() {
	if d.pooled != nil {
		d.pooled.Close()
		d.pooled = nil
	}
	d.session++
}

// verifyRecord returns one of the records in recording, failing with a nice
// error if no such record exists.
func (d *proxyDriver) verifyRecord(recordTyp RecordType) *Record {
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
func (d *proxyDriver) verifyRecordWithStringArg(recordTyp RecordType, arg string) *Record {
	record := d.verifyRecord(recordTyp)
	if record.Args[0].(string) != arg {
		panic(fmt.Sprintf("mismatched argument to %s, expected %s, got %s - regenerate recording",
			recordTyp.String(), arg, record.Args[0].(string)))
	}
	return record
}
