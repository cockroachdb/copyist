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
	"fmt"
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

func TestRollbackWithRecover(t *testing.T) {
	// This bug is only present in playback mode, short circuit if we're
	// recording.
	if copyist.IsRecording() {
		return
	}

	// This is a regression test for a deadlock when copyist would panic upon
	// recording failures. We mount an intentionally out of date source that
	// will fail on any action after opening our transaction. Our transaction
	// helper will attempt a rollback in the case of an error or a panic, which
	// would catch copyist's old behavior of panicking upon out of date
	// recordings.
	// We assert that we hit an out of date error and that rollback is called
	// and returns.
	source := CopyistSource(bytes.NewBuffer([]byte(`
1=DriverOpen	1:nil
2=ConnBegin	1:nil

"TestRollbackWithRecover"=1,2`)))

	defer leaktest.Check(t)()

	var mt mockT

	closer := copyist.OpenSource(&mt, source, t.Name())

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	require.NoError(t, err)
	defer db.Close()

	fnErr, txErr := execTransaction(db, func(tx *sql.Tx) error {
		_, err := tx.Query("SELECT 1")
		return err
	})

	require.EqualError(t, fnErr, "too many calls to ConnQuery\n\nDo you need to regenerate the recording with the -record flag?")
	require.EqualError(t, txErr, "too many calls to TxRollback\n\nDo you need to regenerate the recording with the -record flag?")

	require.NoError(t, closer.Close()) // closer never errors.

	// Verify that the call to .Close invokes t.Fatalf with the first session
	// error that we encountered.
	require.Contains(t, mt.failure, "too many calls to ConnQuery")
	// Verify that t.Fatalf includes the stacktrace leading to the call that
	// triggered the first error. In this case, we look for the error coming
	// from the first closure defined within this test function.
	require.Contains(t, mt.failure, fmt.Sprintf("commontest_test.%s.func1", t.Name()))
}

// execTransaction is a transaction helper function that attempts a rollback in
// the case of panics of errors. It returns both the closure error and the
// error of either commiting or rolling back.
// It is intended to mimic the behavior of
// https://github.com/cockroachdb/cockroach-go/blob/21a237074d6c3c35b68ec43e8d0c9e9ed714d21a/crdb/common.go#L38
func execTransaction(db *sql.DB, fn func(*sql.Tx) error) (fnErr error, txErr error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			txErr = tx.Rollback()
			panic(r)
		}

		if fnErr == nil {
			txErr = tx.Commit()
		} else {
			txErr = tx.Rollback()
		}
	}()

	return fn(tx), nil
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

type mockT struct {
	failure string
}

func (mockT) Name() string { return "" }
func (t *mockT) Fatalf(format string, args ...interface{}) {
	t.failure = fmt.Sprintf(format, args...)
}
