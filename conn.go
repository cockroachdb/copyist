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
	"strings"

	"github.com/jmoiron/sqlx"
)

// proxyConn records and plays back calls to driver.Conn methods.
type proxyConn struct {
	// Conn is a connection to a database. It is not used concurrently
	// by multiple goroutines.
	//
	// Conn is assumed to be stateful.
	driver.Conn

	// SessionResetter may be implemented by Conn to allow drivers to reset the
	// session state associated with the connection and to signal a bad
	// connection.
	driver.SessionResetter

	// driver is a backpointer to the driver that created this connection, used
	// for possibly pooling this connection when it's closed.
	driver *proxyDriver

	// conn is the wrapped "real" connection. It is nil if in playback mode.
	conn driver.Conn

	// name is the data source name passed to Driver.Open. Only connections with
	// the same name can be reused from the driver's connection pool.
	name string

	// session is the copyist session in which this connection was created. This
	// connection can only be reused within that session.
	session *session
}

// ResetSession is called while a connection is in the connection
// pool. No queries will run on this connection until this method returns.
//
// If the connection is bad this should return driver.ErrBadConn to prevent
// the connection from being returned to the connection pool. Any other
// error will be discarded.
//
// proxyConn implements SessionResetter in order to take control of connection
// pooling from the `sql` package. For more information, see the proxyDriver
// comment regarding connection pooling.
func (c *proxyConn) ResetSession(ctx context.Context) error {
	// Return driver.ErrBadConn in order to prevent the `sql` package from
	// pooling this connection. Instead, it will call Close on this connection,
	// at which point the connection can try to return itself to the proxy
	// driver's connection pool instead.
	return driver.ErrBadConn
}

// Prepare returns a prepared statement, bound to this connection.
func (c *proxyConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext returns a prepared statement, bound to this connection.
// context is for the preparation of the statement,
// it must not store the context within the statement itself.
func (c *proxyConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if IsRecording() {
		// TODO(andyk): This is a hack that works around problems with the sqlx
		// library's named args. sqlx uses a hardcoded list of driver names to
		// determine how to represent parameters in prepared queries. For
		// example, postgres uses $1, mysql uses ?, sqlserver uses @, and so on.
		// But since copyist defines a custom driver name, sqlx falls back to
		// the default ?, which won't work with some databases. These issues
		// describe the "custom driver" problem:
		//
		//   https://github.com/jmoiron/sqlx/issues/400
		//   https://github.com/jmoiron/sqlx/issues/559
		//
		// Workaround this problem by rebinding the query if the bind type of
		// the inner driver is different than the default ? character.
		//
		// NOTE: This doesn't work in cases where the parameter character is
		// in a quoted string, etc. Unfortunately, there's not much to be done.
		originalQuery := query
		bindType := sqlx.BindType(c.driver.driverName)
		if bindType != sqlx.QUESTION && strings.IndexByte(query, '?') != -1 {
			query = sqlx.Rebind(bindType, query)
		}

		var stmt driver.Stmt
		var err error
		if prepCtx, ok := c.conn.(driver.ConnPrepareContext); ok {
			stmt, err = prepCtx.PrepareContext(ctx, query)
		} else {
			stmt, err = c.conn.Prepare(query)
		}

		currentSession.AddRecord(&record{Typ: ConnPrepare, Args: recordArgs{originalQuery, err}})
		if err != nil {
			return nil, err
		}
		return &proxyStmt{stmt: stmt}, nil
	}

	rec := currentSession.VerifyRecordWithStringArg(ConnPrepare, query)
	err, _ := rec.Args[1].(error)
	if err != nil {
		return nil, err
	}
	return &proxyStmt{}, nil
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
	// Try to return the connection to the pool rather than closing it.
	if !c.driver.tryPoolConnection(c) {
		// Not successful, so close the connection.
		if IsRecording() {
			return c.conn.Close()
		}
	}
	return nil
}

// Begin starts and returns a new transaction.
//
// Deprecated: Drivers should implement ConnBeginTx instead (or additionally).
func (c *proxyConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx starts and returns a new transaction.
// If the context is canceled by the user the sql package will
// call Tx.Rollback before discarding and closing the connection.
//
// This must check opts.Isolation to determine if there is a set
// isolation level. If the driver does not support a non-default
// level and one is set or if there is a non-default isolation level
// that is not supported, an error must be returned.
//
// This must also check opts.ReadOnly to determine if the read-only
// value is true to either set the read-only transaction property if supported
// or return an error if it is not supported.
func (c *proxyConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if IsRecording() {
		var tx driver.Tx
		var err error
		if beginTx, ok := c.conn.(driver.ConnBeginTx); ok {
			tx, err = beginTx.BeginTx(ctx, opts)
		} else {
			tx, err = c.conn.Begin()
		}

		currentSession.AddRecord(&record{Typ: ConnBegin, Args: recordArgs{err}})
		if err != nil {
			return nil, err
		}
		return &proxyTx{tx: tx}, nil
	}

	rec := currentSession.VerifyRecord(ConnBegin)
	err, _ := rec.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyTx{}, nil
}
