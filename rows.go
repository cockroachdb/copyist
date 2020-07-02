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
	driver *proxyDriver
	rows   driver.Rows
}

// Columns returns the names of the columns. The number of
// columns of the result is inferred from the length of the
// slice. If a particular column name isn't known, an empty
// string should be returned for that entry.
func (r *proxyRows) Columns() []string {
	if r.driver.isRecording() {
		cols := r.rows.Columns()
		r.driver.recording = append(
			r.driver.recording, Record{Typ: RowsColumns, Args: RecordArgs{cols}})
		return cols
	}

	record := r.driver.verifyRecord(RowsColumns)
	return record.Args[0].([]string)
}

// Close closes the rows iterator.
func (r *proxyRows) Close() error {
	if r.driver.isRecording() {
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
	if r.driver.isRecording() {
		var destCopy []driver.Value
		err := r.rows.Next(dest)
		if err == nil {
			destCopy = make([]driver.Value, len(dest))
			copy(destCopy, dest)
		}
		r.driver.recording = append(
			r.driver.recording, Record{Typ: RowsNext, Args: RecordArgs{destCopy, err}})
		return err
	}

	record := r.driver.verifyRecord(RowsNext)
	err, _ := record.Args[1].(error)
	if err != nil {
		return err
	}
	vals := record.Args[0].([]driver.Value)
	copy(dest, vals)
	return nil
}
