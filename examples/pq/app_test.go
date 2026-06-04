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

// This file is a step-by-step walkthrough of using copyist in tests.
//
// copyist has two modes:
//
//	RECORDING: tests run against a real database, and copyist records
//	every SQL driver call into testdata/app_test.copyist.
//
//	    go test ./examples/pq -record
//
//	(Docker must be running; TestMain below starts a throwaway Postgres
//	container automatically. Any other reachable Postgres works too - just
//	change dataSourceName and start it yourself.)
//
//	PLAYBACK (the default): tests replay the recorded calls and never touch
//	a database. No Postgres, no Docker, no network:
//
//	    go test ./examples/pq
//
// The recording file is committed, so playback works immediately after
// cloning this repository.
package pqexample

import (
	"context"
	"database/sql"
	"flag"
	"io"
	"os"
	"testing"

	"github.com/cockroachdb/copyist"
	"github.com/cockroachdb/copyist/drivertest/dockerdb"

	// Import the real driver so it registers itself with database/sql under
	// the name "postgres". copyist wraps it rather than replacing it.
	_ "github.com/lib/pq"
)

const (
	// driverName is the name of the real SQL driver that copyist wraps.
	driverName = "postgres"

	// dataSourceName points at the database used while RECORDING. Playback
	// never dials it. Host port 5433 avoids colliding with a locally
	// installed Postgres on 5432.
	dataSourceName = "postgresql://postgres@localhost:5433/postgres?sslmode=disable"

	// dockerArgs starts a throwaway Postgres while recording.
	dockerArgs = "-p 5433:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres:16"
)

// resetScript puts the database into a clean, well-known state. copyist
// runs it (via SetSessionInit below) at the start of every recording
// session, so each test records against identical data, no matter what
// earlier tests changed.
const resetScript = `
DROP TABLE IF EXISTS customers;
CREATE TABLE customers (id INT PRIMARY KEY, name TEXT);
INSERT INTO customers VALUES (1, 'Andy'), (2, 'Jay'), (3, 'Darin');
`

// Step 1: register a copyist driver that wraps the real one. This creates a
// new database/sql driver named "copyist_postgres". Tests open connections
// through it; depending on the mode it either forwards calls to the real
// driver (recording) or replays the recording file (playback).
func init() {
	copyist.Register(driverName)
}

// Step 2: wire up the test session in TestMain.
func TestMain(m *testing.M) {
	flag.Parse()

	// Tell copyist how to reset the database between recording sessions.
	// This is what makes recorded tests independent of each other. It is
	// only ever invoked in recording mode.
	copyist.SetSessionInit(resetDB)

	// Start a database - but ONLY in recording mode. In playback mode the
	// tests run without any database at all.
	var closer io.Closer
	if copyist.IsRecording() {
		closer = dockerdb.Start(dockerArgs, driverName, dataSourceName)
	}

	code := m.Run()

	// Shut down the container before exiting; deferred calls don't run
	// after os.Exit.
	if closer != nil {
		closer.Close()
	}
	os.Exit(code)
}

// resetDB runs the reset script against the real database.
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

// Step 3: write ordinary tests, with two copyist-specific lines each:
//
//   - `defer copyist.Open(t).Close()` brackets the test in a copyist
//     session: it records, or locates the recording named after this test.
//   - open the database through the "copyist_postgres" driver instead
//     of "postgres".
//
// Everything else is plain database/sql testing.
func TestQueryName(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	name, err := QueryName(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Andy" {
		t.Errorf("expected Andy, got %s", name)
	}
}

// TestQueryNameAt shows that context-aware code works unchanged: the
// context flows through copyist to the underlying driver. There is nothing
// to mock - pass any real context (Background, a test deadline, etc.).
func TestQueryNameAt(t *testing.T) {
	defer copyist.Open(t).Close()

	db, err := sql.Open("copyist_"+driverName, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	name, err := QueryNameAt(context.Background(), db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Jay" {
		t.Errorf("expected Jay, got %s", name)
	}
}
