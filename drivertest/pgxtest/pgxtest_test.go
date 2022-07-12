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

package pgxtest_test

import (
	"database/sql"
	"github.com/cockroachdb/copyist"
	"github.com/fortytw2/leaktest"
	"github.com/jackc/pgconn"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/cockroachdb/copyist/drivertest/commontest"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// TestMain runs all PGX driver-specific tests. To use:
//
//   1. Run the tests with the "-record" command-line flag. This will run the
//      tests against the real PGX driver and create recording files in the
//      testdata directory. This tests generation of recordings.
//   2. Run the test without the "-record" flag. This will run the tests against
//      the copyist driver that plays back the recordings created by step #1.
//      This tests playback of recording.
//
func TestMain(m *testing.M) {
	commontest.RunAllTests(m, "pgx", commontest.PostgresDataSourceName, commontest.PostgresDockerArgs)
}

// TestQuery fetches a single customer.
func TestQuery(t *testing.T) {
	commontest.RunTestQuery(t, "pgx", commontest.PostgresDataSourceName)
}

// TestMultiStatement runs multiple SQL statements in a single Exec/Query
// operation.
func TestMultiStatement(t *testing.T) {
	t.Skip("the pgx driver does not support multiple SQL statements")
	commontest.RunTestMultiStatement(t, "pgx", commontest.PostgresDataSourceName)
}

// TestInsert inserts a row and ensures that it's been committed.
func TestInsert(t *testing.T) {
	commontest.RunTestInsert(t, "pgx", commontest.PostgresDataSourceName)
}

// TestDataTypes queries data types that are interesting for the SQL driver.
func TestDataTypes(t *testing.T) {
	commontest.RunTestDataTypes(t, "pgx", commontest.PostgresDataSourceName)
}

// TestFloatLiterals tests the generation of float literal values, with and
// without fractions and exponents.
func TestFloatLiterals(t *testing.T) {
	// Run twice in order to regress problem with float round-tripping.
	t.Run("run 1", func(t *testing.T) {
		commontest.RunTestFloatLiterals(t, "pgx", commontest.PostgresDataSourceName)
	})
	t.Run("run 2", func(t *testing.T) {
		commontest.RunTestFloatLiterals(t, "pgx", commontest.PostgresDataSourceName)
	})
}

// TestTxns commits and aborts transactions.
func TestTxns(t *testing.T) {
	commontest.RunTestTxns(t, "pgx", commontest.PostgresDataSourceName)
}

// TestSqlx tests usage of the `sqlx` package with copyist.
func TestSqlx(t *testing.T) {
	commontest.RunTestSqlx(t, "pgx", commontest.PostgresDataSourceName)
}

// TestPgConnError tests that pgconn.PgError objects are round-tripped.
func TestPgConnError(t *testing.T) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_pgx", commontest.PostgresDataSourceName)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec("bad query")
	pqErr, ok := err.(*pgconn.PgError)
	require.True(t, ok)
	require.Equal(t, "ERROR", pqErr.Severity)
	require.Equal(t, "42601", pqErr.Code)
	require.Equal(t, "at or near \"bad\": syntax error", pqErr.Message)
	require.Equal(t, "source SQL:\nbad query\n^", pqErr.Detail)
}
