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

// Package pqexample is a complete, runnable example of testing
// database code with copyist and the Postgres lib/pq driver. The
// interesting part is in app_test.go, which walks through the full
// record/playback workflow step by step.
//
// This file contains the "application under test": ordinary database code
// that knows nothing about copyist.
package pqexample

import (
	"context"
	"database/sql"
)

// QueryName returns the name of the customer with the given id. This is
// plain database/sql application code - no copyist imports, nothing special.
func QueryName(db *sql.DB, id int) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM customers WHERE id=$1", id).Scan(&name)
	return name, err
}

// QueryNameAt is the same query, but accepts a context.Context, as
// real-world code usually does. copyist forwards contexts to the underlying
// driver untouched, so context-aware code needs no special handling and no
// mocked contexts - see TestQueryNameAt in app_test.go.
func QueryNameAt(ctx context.Context, db *sql.DB, id int) (string, error) {
	var name string
	err := db.QueryRowContext(ctx, "SELECT name FROM customers WHERE id=$1", id).Scan(&name)
	return name, err
}
