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

package commontest_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/cockroachdb/copyist"
	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/require"
)

// TestBigRecording tests that copyist works with large recordings, and also
// fails when the recording length > copyist.MaxRecordingSize.
func TestBigRecording(t *testing.T) {
	defer leaktest.Check(t)()

	fn := func() {
		closer := copyist.Open(t)
		// Subtle: run the closer in a closure in order to disable the recovery
		// handling.
		defer func() {
			closer.Close()
		}()
		db, _ := sql.Open("copyist_"+driverName, dataSourceName)
		defer db.Close()
		queryBigResult(db)
	}

	// Verify that copyist panics when writing/reading with a lower value of
	// copyist.MaxRecordingSize, but succeeds with a higher value. The only
	// difference between reading and writing is the error message.
	var expected string
	if copyist.IsRecording() {
		expected = "recording exceeds copyist.MaxRecordingSize and cannot be written"
	} else {
		expected = "error parsing recording file: recording exceeds copyist.MaxRecordingSize and cannot be read"
	}

	original := copyist.MaxRecordingSize
	copyist.MaxRecordingSize = 1024
	require.PanicsWithError(t, expected, fn)
	copyist.MaxRecordingSize = original
	fn()
}

func queryBigResult(db *sql.DB) {
	longString := strings.Repeat(" long string ", 64*1024)

	rows, err := db.Query("SELECT $1::TEXT", longString)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var val string
		rows.Scan(&val)
	}
}
