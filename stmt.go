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
	"context"
	"database/sql/driver"
	"errors"
)

// proxyStmt records and plays back calls to driver.Stmt methods.
type proxyStmt struct {
	driver *proxyDriver
	stmt   driver.Stmt
}

// Close closes the statement.
//
// As of Go 1.1, a Stmt will not be closed if it's in use
// by any queries.
func (s *proxyStmt) Close() error {
	if IsRecording() {
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
	if IsRecording() {
		num := s.stmt.NumInput()
		s.driver.recording =
			append(s.driver.recording, &record{Typ: StmtNumInput, Args: recordArgs{num}})
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
	return nil, errors.New("Stmt.Exec is deprecated and no longer supported")
}

// ExecContext executes a query that doesn't return rows, such
// as an INSERT or UPDATE.
//
// ExecContext must honor the context timeout and return when it is canceled.
func (s *proxyStmt) ExecContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Result, error) {
	if IsRecording() {
		var res driver.Result
		var err error
		if execCtx, ok := s.stmt.(driver.StmtExecContext); ok {
			res, err = execCtx.ExecContext(ctx, args)
		} else {
			var vals []driver.Value
			vals, err = namedValueToValue(args)
			if err != nil {
				return nil, err
			}
			res, err = s.stmt.Exec(vals)
		}

		s.driver.recording =
			append(s.driver.recording, &record{Typ: StmtExec, Args: recordArgs{err}})
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
	return nil, errors.New("Stmt.Query is deprecated and no longer supported")
}

// QueryContext executes a query that may return rows, such as a
// SELECT.
//
// QueryContext must honor the context timeout and return when it is canceled.
func (s *proxyStmt) QueryContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Rows, error) {
	if IsRecording() {
		var rows driver.Rows
		var err error
		if stmtCtx, ok := s.stmt.(driver.StmtQueryContext); ok {
			rows, err = stmtCtx.QueryContext(ctx, args)
		} else {
			var vals []driver.Value
			vals, err = namedValueToValue(args)
			if err != nil {
				return nil, err
			}
			rows, err = s.stmt.Query(vals)
		}

		s.driver.recording =
			append(s.driver.recording, &record{Typ: StmtQuery, Args: recordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyRows{driver: s.driver, rows: rows}, nil
	}

	rec := s.driver.verifyRecord(StmtQuery)
	err, _ := rec.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyRows{driver: s.driver}, nil
}

func namedValueToValue(named []driver.NamedValue) ([]driver.Value, error) {
	dargs := make([]driver.Value, len(named))
	for n, param := range named {
		if len(param.Name) > 0 {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		dargs[n] = param.Value
	}
	return dargs, nil
}
