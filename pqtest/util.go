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
	"io"
)

// IndirectOpen is used with the TestIndirectOpen function to test calls to
// copyist.Open in helper functions.
func IndirectOpen(dataSourceName string) (*sql.DB, io.Closer) {
	return evenMoreIndirectOpen(dataSourceName)
}

func evenMoreIndirectOpen(dataSourceName string) (*sql.DB, io.Closer) {
	closer := copyist.Open()

	// Open database.
	db, err := sql.Open("copyist_postgres", dataSourceName)
	if err != nil {
		panic(err)
	}

	return db, closer
}
