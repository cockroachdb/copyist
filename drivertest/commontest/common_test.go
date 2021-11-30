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

package commontest_test

import (
	"bytes"
	"database/sql"
	"io"
	"testing"

	"github.com/cockroachdb/copyist"
	"github.com/cockroachdb/copyist/drivertest/commontest"
	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

// Arbitrarily use PQ driver for tests that aren't driver-specific.
const (
	driverName     = "postgres"
	dataSourceName = "postgresql://root@localhost:26888?sslmode=disable"

	// Don't use default CRDB port in case another instance is already running.
	dockerArgs = "-p 26888:26257 cockroachdb/cockroach:v20.2.4 start-single-node --insecure"
)

func TestMain(m *testing.M) {
	commontest.RunAllTests(m, driverName, dataSourceName, dockerArgs)
}

// TestIndirectOpen calls copyist.Open indirectly in a helper function.
func TestIndirectOpen(t *testing.T) {
	defer leaktest.Check(t)()
	db, closer := indirectOpen(t, dataSourceName)
	defer closer.Close()
	defer db.Close()

	rows, err := db.Query("SELECT name FROM customers WHERE id=$1", 1)
	require.NoError(t, err)
	defer rows.Close()
	rows.Next()

	var name string
	rows.Scan(&name)
	require.Equal(t, "Andy", name)
}

func TestOpenNamed(t *testing.T) {
	defer leaktest.Check(t)()
	defer copyist.OpenNamed(t, "recording.txt", "TestOpenNamed").Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT 1")
	require.NoError(t, err)
	rows.Next()
}

func TestOpenReadWriteCloser(t *testing.T) {
	source := CopyistSource(bytes.NewBuffer([]byte(`
1=DriverOpen	1:nil
2=ConnQuery	2:"SELECT 1"	1:nil
3=RowsColumns	9:["?column?"]
4=RowsNext	11:[4:1]	1:nil

"TestOpenSource"=1,2,3,4`)))

	defer leaktest.Check(t)()
	defer copyist.OpenSource(t, source, "TestOpenSource").Close()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT 1")
	require.NoError(t, err)
	rows.Next()
}

func TestIsOpen(t *testing.T) {
	require.False(t, copyist.IsOpen())

	closer := copyist.Open(t)
	require.True(t, copyist.IsOpen())

	closer.Close()
	require.False(t, copyist.IsOpen())
}

// TestPooling ensures that sessions are pooled in the same copyist session, but
// not across copyist sessions.
func TestPooling(t *testing.T) {
	defer leaktest.Check(t)()

	// Open database.
	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	var sessionID string

	t.Run("ensure connections are pooled within same copyist session", func(t *testing.T) {
		defer copyist.Open(t).Close()

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
		defer copyist.Open(t).Close()

		var nextSessionID string
		rows, err := db.Query("SHOW session_id")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&nextSessionID))
		require.NotEqual(t, sessionID, nextSessionID)
		rows.Close()
	})
}

type copyistSource struct {
	io.Reader
}

func (s copyistSource) ReadAll() ([]byte, error) {
	return io.ReadAll(s.Reader)
}

func (s copyistSource) WriteAll([]byte) error {
	return nil
}

func CopyistSource(r io.Reader) copyist.Source {
	return copyistSource{r}
}
