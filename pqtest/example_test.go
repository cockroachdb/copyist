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
	"testing"

	"github.com/cockroachdb/copyist"
)

func TestQueryName(t *testing.T) {
	defer copyist.Open().Close()

	db, _ := sql.Open("copyist_postgres", dataSourceName)
	defer db.Close()

	name := QueryName(db)
	if name != "Andy" {
		t.Error("failed test")
	}
}

func QueryName(db *sql.DB) string {
	rows, err := db.Query("SELECT name FROM customers WHERE id=$1", 1)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		return name
	}
	return ""
}
