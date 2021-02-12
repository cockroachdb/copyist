// Copyright 2021 The Cockroach Authors.
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

package commontest

import (
	"database/sql"
	"flag"
	"github.com/fortytw2/leaktest"
	"io"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/copyist"
	"github.com/cockroachdb/copyist/drivertest/dockerdb"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

// DataTypes contains many interesting data types that can be returned by SQL
// drivers.
type DataTypes struct {
	Int              int
	Str, Dec, FltArr string
	TimeZ, Time      time.Time
	Bool             bool
	Bytes            []byte
	Flt              float64
	Uuid             []byte
}

// resetScript is a generic SQL script that resets the database to a clean
// state and creates some simple fixtures for common tests to use.
const resetScript = `
DROP TABLE IF EXISTS customers;
CREATE TABLE customers (id INT PRIMARY KEY, name TEXT);
INSERT INTO customers VALUES (1, 'Andy'), (2, 'Jay'), (3, 'Darin');

DROP TABLE IF EXISTS datatypes;
`

// RunTests is called by other driver-specific test packages (like pgxtest and
// pqtest) in order to set up the test environment and then run all tests. It
// registers a copyist driver and starts up a SQL docker instance if in
// recording mode. It then runs all tests by calling testing.M.Run(), and
// finally exits the process when complete.
func RunAllTests(m *testing.M, driverName, dataSourceName, dockerArgs string) {
	flag.Parse()

	copyist.Register(driverName, func() {
		db, err := sql.Open(driverName, dataSourceName)
		if err != nil {
			panic(err)
		}
		defer db.Close()
		if _, err := db.Exec(resetScript); err != nil {
			panic(err)
		}
	})

	// If in recording mode, then run database in docker container until test is
	// complete.
	var closer io.Closer
	if copyist.IsRecording() {
		closer = dockerdb.Start(dockerArgs, driverName, dataSourceName)
	}

	code := m.Run()

	// Close the docker container before calling os.Exit; defers don't get
	// called in that case.
	if closer != nil {
		closer.Close()
	}

	os.Exit(code)
}

// RunTestQuery fetches a single customer.
func RunTestQuery(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT name FROM customers WHERE id=$1", 1)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		require.Equal(t, "Andy", name)
	}
}

// RunTestInsert inserts a row and ensures that it's been committed.
func RunTestInsert(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

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

// RunTestDataTypes queries data types that are interesting for the SQL driver.
func RunTestDataTypes(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	// Construct table with many data types.
	res, err := db.Exec(`
		CREATE TABLE datatypes
		(i INT, s TEXT, tz TIMESTAMPTZ, t TIMESTAMP, b BOOL,
		 by BYTES, f FLOAT, d DECIMAL, fa FLOAT[], u UUID)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO datatypes VALUES
			(1, 'foo' || CHR(9) || CHR(10) || ' ,]', '2000-01-01T10:00:00Z', '2000-01-01T10:00:00Z',
			 true, 'ABCD', 1.1, 100.1234, ARRAY(1.1, 1.2345678901234567),
			 '8B78978B-7D8B-489E-8CA9-AC4BDC495A82'),
			(2, '', '2000-02-02T11:11:11-08:00', '2000-02-02T11:11:11-08:00', false,
			 '', -1e10, -0.0, ARRAY(), '00000000-0000-0000-0000-000000000000')
	`)
	require.NoError(t, err)

	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(0), affected)

	var out DataTypes
	rows, err := db.Query("SELECT i, s, tz, t, b, by, f, d, fa, u FROM datatypes")
	require.NoError(t, err)

	rows.Next()
	require.NoError(t, rows.Scan(
		&out.Int, &out.Str, &out.TimeZ, &out.Time, &out.Bool, &out.Bytes,
		&out.Flt, &out.Dec, &out.FltArr, &out.Uuid))
	require.Equal(t, DataTypes{
		Int: 1, Str: "foo\t\n ,]", TimeZ: parseTime("2000-01-01T10:00:00Z", driverName),
		Time: parseTime("2000-01-01T10:00:00+00:00", driverName), Bool: true,
		Bytes: []byte{'A', 'B', 'C', 'D'}, Flt: 1.1, Dec: "100.1234",
		FltArr: "{1.1,1.2345678901234567}", Uuid: []byte("8b78978b-7d8b-489e-8ca9-ac4bdc495a82"),
	}, out)

	rows.Next()
	require.NoError(t, rows.Scan(
		&out.Int, &out.Str, &out.TimeZ, &out.Time, &out.Bool, &out.Bytes,
		&out.Flt, &out.Dec, &out.FltArr, &out.Uuid))
	require.Equal(t, DataTypes{
		Int: 2, Str: "", TimeZ: parseTime("2000-02-02T19:11:11Z", driverName),
		Time: parseTime("2000-02-02T11:11:11+00:00", driverName), Bool: false,
		Bytes: []byte{}, Flt: -1e10, Dec: "0.0", FltArr: "{}",
		Uuid: []byte("00000000-0000-0000-0000-000000000000"),
	}, out)

	rows.Close()
}

// RunTestFloatLiterals tests the generation of float literal values, with and
// without fractions and exponents.
func RunTestFloatLiterals(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT 1::float, 1.1::float, 1e20::float")
	require.NoError(t, err)
	rows.Next()
}

// RunTestTxns commits and aborts transactions.
func RunTestTxns(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

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

// RunTestSqlx tests usage of the `sqlx` package with copyist.
func RunTestSqlx(t *testing.T, driverName, dataSourceName string) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sqlx.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()
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

func parseTime(s string, driverName string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	if driverName == "pgx" {
		if t.Location() == time.UTC {
			t = t.Local()
		} else {
			t = t.UTC()
		}
	}
	return t
}
