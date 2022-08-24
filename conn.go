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

// ExecContext executes a query that doesn't return rows, such
// as an INSERT or UPDATE.
//
// ExecContext must honor the context timeout and return when it is canceled.
func (c *proxyConn) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	if IsRecording() {
		var res driver.Result
		var err error
		switch t := c.conn.(type) {
		case driver.ExecerContext:
			res, err = t.ExecContext(ctx, query, args)
		case driver.Execer:
			var vals []driver.Value
			vals, err = namedValueToValue(args)
			if err != nil {
				return nil, err
			}
			res, err = t.Exec(query, vals)
		default:
			return nil, driver.ErrSkip
		}

		currentSession.AddRecord(&record{Typ: ConnExec, Args: recordArgs{query, err}})
		if err != nil {
			return nil, err
		}
		return &proxyResult{res: res}, nil
	}

	rec, err := currentSession.VerifyRecordWithStringArg(ConnExec, query)
	if err != nil {
		return nil, err
	}
	err, _ = rec.Args[1].(error)
	if err != nil {
		return nil, err
	}
	return &proxyResult{}, nil
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
		var stmt driver.Stmt
		var err error
		if prepCtx, ok := c.conn.(driver.ConnPrepareContext); ok {
			stmt, err = prepCtx.PrepareContext(ctx, query)
		} else {
			stmt, err = c.conn.Prepare(query)
		}

		currentSession.AddRecord(&record{Typ: ConnPrepare, Args: recordArgs{query, err}})
		if err != nil {
			return nil, err
		}
		return &proxyStmt{stmt: stmt}, nil
	}

	rec, err := currentSession.VerifyRecordWithStringArg(ConnPrepare, query)
	if err != nil {
		return nil, err
	}
	err, _ = rec.Args[1].(error)
	if err != nil {
		return nil, err
	}
	return &proxyStmt{}, nil
}

// QueryContext executes a query that may return rows, such as a
// SELECT.
//
// QueryContext must honor the context timeout and return when it is canceled.
func (c *proxyConn) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	if IsRecording() {
		var rows driver.Rows
		var err error
		switch t := c.conn.(type) {
		case driver.QueryerContext:
			rows, err = t.QueryContext(ctx, query, args)
		case driver.Queryer:
			var vals []driver.Value
			vals, err = namedValueToValue(args)
			if err != nil {
				return nil, err
			}
			rows, err = t.Query(query, vals)
		default:
			return nil, driver.ErrSkip
		}

		currentSession.AddRecord(&record{Typ: ConnQuery, Args: recordArgs{query, err}})
		if err != nil {
			return nil, err
		}
		return &proxyRows{rows: rows}, nil
	}

	rec, err := currentSession.VerifyRecordWithStringArg(ConnQuery, query)
	if err != nil {
		return nil, err
	}
	err, _ = rec.Args[1].(error)
	if err != nil {
		return nil, err
	}
	return &proxyRows{}, nil
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

	rec, err := currentSession.VerifyRecord(ConnBegin)
	if err != nil {
		return nil, err
	}
	err, _ = rec.Args[0].(error)
	if err != nil {
		return nil, err
	}
	return &proxyTx{}, nil
}
