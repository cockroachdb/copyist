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

package pqtest

import (
	"database/sql"
	"testing"

	"github.com/lib/pq"

	"github.com/cockroachdb/copyist"
	"github.com/cockroachdb/copyist/drivertest/commontest"
	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

// TestMain runs all PQ driver-specific tests. To use:
//
//   1. Run the tests with the "-record" command-line flag. This will run the
//      tests against the real PQ driver and create recording files in the
//      testdata directory. This tests generation of recordings.
//   2. Run the test without the "-record" flag. This will run the tests against
//      the copyist driver that plays back the recordings created by step #1.
//      This tests playback of recording.
//
func TestMain(m *testing.M) {
	commontest.RunAllTests(m, "postgres", commontest.PostgresDataSourceName, commontest.PostgresDockerArgs)
}

// TestQuery fetches a single customer.
func TestQuery(t *testing.T) {
	commontest.RunTestQuery(t, "postgres", commontest.PostgresDataSourceName)
}

// TestMultiStatement runs multiple SQL statements in a single Exec/Query
// operation.
func TestMultiStatement(t *testing.T) {
	commontest.RunTestMultiStatement(t, "postgres", commontest.PostgresDataSourceName)
}

// TestInsert inserts a row and ensures that it's been committed.
func TestInsert(t *testing.T) {
	commontest.RunTestInsert(t, "postgres", commontest.PostgresDataSourceName)
}

// TestDataTypes queries data types that are interesting for the SQL driver.
func TestDataTypes(t *testing.T) {
	commontest.RunTestDataTypes(t, "postgres", commontest.PostgresDataSourceName)
}

// TestFloatLiterals tests the generation of float literal values, with and
// without fractions and exponents.
func TestFloatLiterals(t *testing.T) {
	// Run twice in order to regress problem with float round-tripping.
	t.Run("run 1", func(t *testing.T) {
		commontest.RunTestFloatLiterals(t, "postgres", commontest.PostgresDataSourceName)
	})
	t.Run("run 2", func(t *testing.T) {
		commontest.RunTestFloatLiterals(t, "postgres", commontest.PostgresDataSourceName)
	})
}

// TestTxns commits and aborts transactions.
func TestTxns(t *testing.T) {
	commontest.RunTestTxns(t, "postgres", commontest.PostgresDataSourceName)
}

// TestSqlx tests usage of the `sqlx` package with copyist.
func TestSqlx(t *testing.T) {
	commontest.RunTestSqlx(t, "postgres", commontest.PostgresDataSourceName)
}

// TestPqError tests that pq.Error objects are round-tripped.
func TestPqError(t *testing.T) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", commontest.PostgresDataSourceName)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec("bad query")
	pqErr, ok := err.(*pq.Error)
	require.True(t, ok)
	require.Equal(t, "ERROR", pqErr.Severity)
	require.Equal(t, pq.ErrorCode("42601"), pqErr.Code)
	require.Equal(t, "at or near \"bad\": syntax error", pqErr.Message)
	require.Equal(t, "source SQL:\nbad query\n^", pqErr.Detail)
}
