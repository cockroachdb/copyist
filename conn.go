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

// proxyConn records and plays back calls to driver.Conn methods.
type proxyConn struct {
	driver *proxyDriver
	conn   driver.Conn
}

// Prepare returns a prepared statement, bound to this connection.
func (c *proxyConn) Prepare(query string) (driver.Stmt, error) {
	if c.driver.isRecording() {
		stmt, err := c.conn.Prepare(query)
		c.driver.recording = append(
			c.driver.recording, Record{Typ: ConnPrepare, Args: RecordArgs{query, err}})
		if err != nil {
			return nil, err
		}
		return &proxyStmt{driver: c.driver, stmt: stmt}, nil
	}

	record := c.driver.verifyRecordWithStringArg(ConnPrepare, query)
	err, _ := record.Args[1].(error)
	if err != nil {
		return nil, err
	}
	return &proxyStmt{driver: c.driver}, nil
}

// Close invalidates and potentially stops any current
// prepared statements and transactions, marking this
// connection as no longer in use.
//
// Because the sql package maintains a free pool of
// connections and only calls Close when there's a surplus of
// idle connections, it shouldn't be necessary for drivers to
// do their own connection caching.
func (c *proxyConn) Close() error {
	if c.driver.isRecording() {
		return c.conn.Close()
	}
	return nil
}

// Begin starts and returns a new transaction.
//
// Deprecated: Drivers should implement ConnBeginTx instead (or additionally).
func (c *proxyConn) Begin() (driver.Tx, error) {
	if c.driver.isRecording() {
		tx, err := c.conn.Begin()
		c.driver.recording = append(c.driver.recording, Record{Typ: ConnBegin, Args: RecordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyTx{driver: c.driver, tx: tx}, nil
	}

	record := c.driver.verifyRecord(ConnBegin)
	err, _ := record.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyTx{driver: c.driver}, nil
}
