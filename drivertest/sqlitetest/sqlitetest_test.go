// Copyright 2026 The Cockroach Authors.
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

// sqlitetest tests copyist against the cgo-free modernc.org/sqlite driver.
// Unlike the Postgres test packages, no docker container is needed: in
// recording mode the tests run against a local temporary database file.
//
// To use:
//
//  1. Run the tests with COPYIST_RECORD=1. This will run the tests against
//     the real sqlite driver and create recording files in the testdata
//     directory. This tests generation of recordings.
//  2. Run the tests without COPYIST_RECORD. This will run the tests against
//     the copyist driver that plays back the recordings created by step #1.
//     This tests playback of recordings.
package sqlitetest

import (
	"database/sql"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/copyist"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const (
	// driverName is the name the modernc.org/sqlite driver registers itself
	// under with the `sql` package.
	driverName = "sqlite"

	// dataSourceName points to a local database file. An in-memory database
	// (":memory:") cannot be used, because each new connection would see a
	// fresh, empty database, and copyist deliberately re-opens connections
	// for each session in order to keep recordings deterministic.
	dataSourceName = "file:copyist_test.db"
)

// resetScript resets the database to a clean, well-known state. It is run by
// copyist at the start of each session in recording mode.
const resetScript = `
DROP TABLE IF EXISTS customers;
CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);
INSERT INTO customers VALUES (1, 'Andy'), (2, 'Jay'), (3, 'Darin');
DROP TABLE IF EXISTS datatypes;
`

func TestMain(m *testing.M) {
	flag.Parse()
	copyist.Register(driverName)
	copyist.SetSessionInit(resetDB)
	os.Exit(m.Run())
}

// resetDB runs the reset script against the real database. Only called in
// recording mode.
func resetDB() {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if _, err := db.Exec(resetScript); err != nil {
		panic(err)
	}
}

// TestQuery fetches a single customer.
func TestQuery(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT name FROM customers WHERE id=?", 1)
	require.NoError(t, err)
	defer rows.Close()

	require.True(t, rows.Next())
	var name string
	require.NoError(t, rows.Scan(&name))
	require.Equal(t, "Andy", name)
	require.False(t, rows.Next())
}

// TestInsert inserts a row and ensures that it's been committed.
func TestInsert(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	res, err := db.Exec("INSERT INTO customers VALUES (4, 'Joe')")
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	var cnt int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM customers").Scan(&cnt))
	require.Equal(t, 4, cnt)
}

// TestMultiStatement runs multiple SQL statements in a single Exec operation.
func TestMultiStatement(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec("INSERT INTO customers VALUES (4, 'Joe'); INSERT INTO customers VALUES (5, 'Mary')")
	require.NoError(t, err)

	var cnt int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM customers").Scan(&cnt))
	require.Equal(t, 5, cnt)
}

// TestTxns commits and aborts transactions.
func TestTxns(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	// Commit a transaction.
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO customers VALUES (4, 'Joe')")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	// Roll back a transaction.
	tx, err = db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO customers VALUES (5, 'Mary')")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	var cnt int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM customers").Scan(&cnt))
	require.Equal(t, 4, cnt)
}

// TestDataTypes round-trips the SQLite storage classes (INTEGER, REAL, TEXT,
// BLOB) plus time values through recording and playback.
func TestDataTypes(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec("CREATE TABLE datatypes (i INTEGER, r REAL, t TEXT, b BLOB, ts TIMESTAMP)")
	require.NoError(t, err)

	when := time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err = db.Exec("INSERT INTO datatypes VALUES (?, ?, ?, ?, ?)",
		int64(1), 1.5, "foo", []byte{1, 2, 3}, when)
	require.NoError(t, err)

	var (
		i  int64
		r  float64
		s  string
		b  []byte
		ts time.Time
	)
	require.NoError(t, db.QueryRow("SELECT i, r, t, b, ts FROM datatypes").
		Scan(&i, &r, &s, &b, &ts))
	require.Equal(t, int64(1), i)
	require.Equal(t, 1.5, r)
	require.Equal(t, "foo", s)
	require.Equal(t, []byte{1, 2, 3}, b)
	require.True(t, when.Equal(ts))
}

// TestSqlx tests usage of the `sqlx` package with copyist. sqlx does not know
// the "sqlite" driver name (only "sqlite3"), so it falls back to the UNKNOWN
// bind type, which leaves `?` placeholders untouched - exactly what SQLite
// expects.
func TestSqlx(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sqlx.Open("copyist_sqlite", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	type Customer struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}

	var cust Customer
	require.NoError(t, db.Get(&cust, "SELECT id, name FROM customers WHERE id=?", 1))
	require.Equal(t, Customer{ID: 1, Name: "Andy"}, cust)
}
