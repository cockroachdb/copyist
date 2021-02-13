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
	"github.com/cockroachdb/copyist"
	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/cockroachdb/copyist/drivertest/commontest"

	_ "github.com/jackc/pgx/v4/stdlib"
	_ "github.com/lib/pq"
)

// TestMain runs tests that use multiple drivers within the same Copyist
// session.
func TestMain(m *testing.M) {
	// Register PGX driver and then have RunAllTests register the PQ driver.
	copyist.Register("pgx")
	commontest.RunAllTests(m, "postgres", commontest.PostgresDataSourceName, commontest.PostgresDockerArgs)
}

// TestMultipleDrivers uses two different drivers in same test, with interleaved
// calls between them.
func TestMultipleDrivers(t *testing.T) {
	defer leaktest.Check(t)()
	defer copyist.Open(t).Close()

	openDB := func(driverName string) *sql.DB {
		db, err := sql.Open("copyist_"+driverName, commontest.PostgresDataSourceName)
		require.NoError(t, err)
		return db
	}

	queryDB := func(db *sql.DB, id int, expected string) {
		rows, err := db.Query("SELECT name FROM customers WHERE id=$1", id)
		require.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var name string
			rows.Scan(&name)
			require.Equal(t, expected, name)
		}
	}

	// Open database using each driver.
	db := openDB("postgres")
	defer db.Close()
	db2 := openDB("pgx")
	defer db2.Close()

	// Query using each driver.
	queryDB(db, 1, "Andy")
	queryDB(db2, 1, "Andy")

	// And again.
	queryDB(db, 2, "Jay")
	queryDB(db2, 2, "Jay")
}
