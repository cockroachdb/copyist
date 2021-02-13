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

package copyist

import "database/sql/driver"

// proxyRows records and plays back calls to driver.Rows methods.
type proxyRows struct {
	// Rows is an iterator over an executed query's results.
	driver.Rows

	rows driver.Rows
}

// Columns returns the names of the columns. The number of
// columns of the result is inferred from the length of the
// slice. If a particular column name isn't known, an empty
// string should be returned for that entry.
func (r *proxyRows) Columns() []string {
	if IsRecording() {
		cols := r.rows.Columns()
		currentSession.AddRecord(&record{Typ: RowsColumns, Args: recordArgs{cols}})
		return cols
	}

	rec := currentSession.VerifyRecord(RowsColumns)
	return rec.Args[0].([]string)
}

// Close closes the rows iterator.
func (r *proxyRows) Close() error {
	if IsRecording() {
		return r.rows.Close()
	}
	return nil
}

// Next is called to populate the next row of data into
// the provided slice. The provided slice will be the same
// size as the Columns() are wide.
//
// Next should return io.EOF when there are no more rows.
//
// The dest should not be written to outside of Next. Care
// should be taken when closing Rows not to modify
// a buffer held in dest.
func (r *proxyRows) Next(dest []driver.Value) error {
	if IsRecording() {
		var destCopy []driver.Value
		err := r.rows.Next(dest)
		if err == nil {
			destCopy = make([]driver.Value, len(dest))
			for i := range dest {
				destCopy[i] = deepCopyValue(dest[i])
			}
		}
		currentSession.AddRecord(&record{Typ: RowsNext, Args: recordArgs{destCopy, err}})
		return err
	}

	rec := currentSession.VerifyRecord(RowsNext)
	err, _ := rec.Args[1].(error)
	if err != nil {
		return err
	}
	vals := rec.Args[0].([]driver.Value)
	copy(dest, vals)
	return nil
}
