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

import "database/sql/driver"

// proxyStmt records and plays back calls to driver.Stmt methods.
type proxyStmt struct {
	// Stmt is a prepared statement. It is bound to a Conn and not
	// used by multiple goroutines concurrently.
	driver.Stmt

	driver *proxyDriver
	stmt   driver.Stmt
}

// Close closes the statement.
//
// As of Go 1.1, a Stmt will not be closed if it's in use
// by any queries.
func (s *proxyStmt) Close() error {
	if s.driver.isRecording() {
		return s.stmt.Close()
	}
	return nil
}

// NumInput returns the number of placeholder parameters.
//
// If NumInput returns >= 0, the sql package will sanity check
// argument counts from callers and return errors to the caller
// before the statement's Exec or Query methods are called.
//
// NumInput may also return -1, if the driver doesn't know
// its number of placeholders. In that case, the sql package
// will not sanity check Exec or Query argument counts.
func (s *proxyStmt) NumInput() int {
	if s.driver.isRecording() {
		num := s.stmt.NumInput()
		s.driver.recording = append(s.driver.recording, Record{Typ: StmtNumInput, Args: RecordArgs{num}})
		return num
	}

	record := s.driver.verifyRecord(StmtNumInput)
	return record.Args[0].(int)
}

// Exec executes a query that doesn't return rows, such
// as an INSERT or UPDATE.
//
// Deprecated: Drivers should implement StmtExecContext instead (or additionally).
func (s *proxyStmt) Exec(args []driver.Value) (driver.Result, error) {
	if s.driver.isRecording() {
		res, err := s.stmt.Exec(args)
		s.driver.recording = append(s.driver.recording, Record{Typ: StmtExec, Args: RecordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyResult{driver: s.driver, res: res}, nil
	}

	record := s.driver.verifyRecord(StmtExec)
	err, _ := record.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyResult{driver: s.driver}, nil
}

// Query executes a query that may return rows, such as a
// SELECT.
//
// Deprecated: Drivers should implement StmtQueryContext instead (or additionally).
func (s *proxyStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s.driver.isRecording() {
		rows, err := s.stmt.Query(args)
		s.driver.recording = append(s.driver.recording, Record{Typ: StmtQuery, Args: RecordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyRows{driver: s.driver, rows: rows}, nil
	}

	record := s.driver.verifyRecord(StmtQuery)
	err, _ := record.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyRows{driver: s.driver}, nil
}
