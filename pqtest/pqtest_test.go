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

package pqtest_test

import (
	"database/sql"
	"flag"
	"io"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/copyist"
	"github.com/cockroachdb/copyist/dockerdb"
	"github.com/cockroachdb/copyist/pqtest"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

// Don't use default CRDB port in case another instance is already running.
const dockerArgs = "-p 26888:26257 cockroachdb/cockroach:v20.1.3 start --insecure"
const dataSourceName = "postgresql://root@localhost:26888?sslmode=disable"

const resetScript = `
DROP TABLE IF EXISTS customers;
CREATE TABLE customers (id INT PRIMARY KEY, name TEXT);
INSERT INTO customers VALUES (1, 'Andy'), (2, 'Jay'), (3, 'Darin');

DROP TABLE IF EXISTS datatypes;
`

type dataTypes struct {
	i        int
	s, d, fa string
	tmz, tm  time.Time
	b        bool
	by       []uint8
	f        float64
}

// TestMain registers a copyist driver and starts up a CRDB docker instance if
// in recording mode. To run the pq tests, follow these steps:
//
//   1. Run the tests with the "-record" command-line flag. This will run the
//      tests against the real PG driver and update the pqtest_sql_test.go file
//      with recordings for each test. This tests generation of recordings.
//   2. Run the test without the "-record" flag. This will run the tests against
//      the copyist driver that plays back the recordings created by step #1.
//      This tests playback of recording.
//
func TestMain(m *testing.M) {
	flag.Parse()

	// If in recording mode, then run database in docker container until test is
	// complete.
	var closer io.Closer
	if copyist.IsRecording() {
		closer = dockerdb.Start(dockerArgs, "postgres", dataSourceName)
	}

	copyist.Register("postgres", resetDB)

	code := m.Run()

	// Close the docker container before calling os.Exit; defers don't get
	// called in that case.
	if closer != nil {
		closer.Close()
	}

	os.Exit(code)
}

// resetDB runs the DB reset scripts, which resets the database before each
// test.
func resetDB() {
	db, err := sql.Open("postgres", dataSourceName)
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
	defer copyist.Open().Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)

	rows, err := db.Query("SELECT name FROM customers WHERE id=$1", 1)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		require.Equal(t, "Andy", name)
	}
}

// TestInsert inserts a row and ensures that it's been committed.
func TestInsert(t *testing.T) {
	defer copyist.Open().Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)

	res, err := db.Exec("INSERT INTO customers VALUES ($1, $2)", 4, "Joel")
	require.NoError(t, err)

	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	rows, err := db.Query("SELECT COUNT(*) FROM customers")
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var cnt int
		rows.Scan(&cnt)
		require.Equal(t, 4, cnt)
	}
}

// TestDataTypes queries data types that are interesting for the PQ driver.
func TestDataTypes(t *testing.T) {
	defer copyist.Open().Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)

	// Construct table with many data types.
	res, err := db.Exec(`
		CREATE TABLE datatypes
		(i INT, s TEXT, tz TIMESTAMPTZ, t TIMESTAMP, b BOOL,
		 by BYTES, f FLOAT, d DECIMAL, fa FLOAT[])
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO datatypes VALUES
			(1, 'foo', '2000-01-01T10:00:00Z', '2000-01-01T10:00:00Z', true,
			 'ABCD', 1.1, 100.1234, ARRAY(1.1, 2.2)),
			(2, '', '2000-02-02T11:11:11-08:00', '2000-02-02T11:11:11-08:00', false,
			 '', -1e10, -0.0, ARRAY())
	`)
	require.NoError(t, err)

	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(0), affected)

	var out dataTypes
	rows, err := db.Query("SELECT i, s, tz, t, b, by, f, d, fa FROM datatypes")
	require.NoError(t, err)

	rows.Next()
	require.NoError(
		t, rows.Scan(&out.i, &out.s, &out.tmz, &out.tm, &out.b, &out.by, &out.f, &out.d, &out.fa))
	require.Equal(t, dataTypes{
		i: 1, s: "foo", tmz: copyist.ParseTime("2000-01-01T10:00:00Z"),
		tm: copyist.ParseTime("2000-01-01T10:00:00+00:00"), b: true,
		by: []byte{'A', 'B', 'C', 'D'}, f: 1.1, d: "100.1234", fa: "{1.1,2.2}",
	}, out)

	rows.Next()
	require.NoError(
		t, rows.Scan(&out.i, &out.s, &out.tmz, &out.tm, &out.b, &out.by, &out.f, &out.d, &out.fa))
	require.Equal(t, dataTypes{
		i: 2, s: "", tmz: copyist.ParseTime("2000-02-02T19:11:11Z"),
		tm: copyist.ParseTime("2000-02-02T11:11:11+00:00"), b: false,
		by: []byte{}, f: -1e10, d: "0.0", fa: "{}",
	}, out)

	rows.Close()
}

// TestTxns commits and aborts transactions.
func TestTxns(t *testing.T) {
	defer copyist.Open().Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)

	// Commit a transaction.
	tx, err := db.Begin()
	require.NoError(t, err)

	_, err = tx.Exec("INSERT INTO customers VALUES ($1, $2)", 4, "Joel")
	require.NoError(t, err)

	require.NoError(t, tx.Commit())

	// Abort a transaction.
	tx, err = db.Begin()
	require.NoError(t, err)

	_, err = tx.Exec("INSERT INTO customers VALUES ($1, $2)", 5, "Josh")
	require.NoError(t, err)

	require.NoError(t, tx.Rollback())

	// Verify count.
	rows, err := db.Query("SELECT COUNT(*) FROM customers")
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var cnt int
		rows.Scan(&cnt)
		require.Equal(t, 4, cnt)
	}
}

// TestPooling ensures that sessions are pooled in the same copyist session, but
// not across copyist sessions.
func TestPooling(t *testing.T) {
	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)

	var sessionID string

	t.Run("ensure connections are pooled within same copyist session", func(t *testing.T) {
		defer copyist.Open().Close()

		var firstSessionID string
		rows, err := db.Query("SHOW session_id")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&firstSessionID))
		require.False(t, rows.Next())
		rows.Close()

		rows, err = db.Query("SHOW session_id")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&sessionID))
		require.False(t, rows.Next())
		require.Equal(t, firstSessionID, sessionID)
		rows.Close()
	})

	t.Run("ensure connections are *not* pooled across copyist sessions", func(t *testing.T) {
		defer copyist.Open().Close()

		var nextSessionID string
		rows, err := db.Query("SHOW session_id")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&nextSessionID))
		require.NotEqual(t, sessionID, nextSessionID)
		rows.Close()
	})
}

// TestIndirectOpen calls copyist.Open indirectly in a helper function.
func TestIndirectOpen(t *testing.T) {
	db, closer := pqtest.IndirectOpen(dataSourceName)
	defer closer.Close()

	rows, err := db.Query("SELECT name FROM customers WHERE id=$1", 1)
	require.NoError(t, err)
	defer rows.Close()
	rows.Next()

	var name string
	rows.Scan(&name)
	require.Equal(t, "Andy", name)
}

// TestSqlx tests usage of the `sqlx` package with copyist.
func TestSqlx(t *testing.T) {
	defer copyist.Open().Close()

	// Open database.
	db, err := sqlx.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)
	tx, err := db.Beginx()
	require.NoError(t, err)

	// Named query.
	cust := struct{ Id int }{Id: 1}
	rows, err := tx.NamedQuery("SELECT name FROM customers WHERE id=:id", cust)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		require.Equal(t, "Andy", name)
	}

	require.NoError(t, tx.Commit())
}
