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

// proxyResult records and plays back calls to driver.Result methods.
type proxyResult struct {
	// Result is the result of a query execution.
	driver.Result

	driver *proxyDriver
	res    driver.Result
}

// LastInsertId returns the database's auto-generated ID
// after, for example, an INSERT into a table with primary
// key.
func (r *proxyResult) LastInsertId() (int64, error) {
	if IsRecording() {
		id, err := r.res.LastInsertId()
		r.driver.recording = append(
			r.driver.recording, &record{Typ: ResultLastInsertId, Args: recordArgs{id, err}})
		return id, err
	}

	record := r.driver.verifyRecord(ResultLastInsertId)
	err, _ := record.Args[1].(error)
	if err != nil {
		return 0, err
	}
	return record.Args[0].(int64), nil
}

// RowsAffected returns the number of rows affected by the
// query.
func (r *proxyResult) RowsAffected() (int64, error) {
	if IsRecording() {
		affected, err := r.res.RowsAffected()
		r.driver.recording = append(
			r.driver.recording, &record{Typ: ResultRowsAffected, Args: recordArgs{affected, err}})
		return affected, err
	}

	record := r.driver.verifyRecord(ResultRowsAffected)
	err, _ := record.Args[1].(error)
	if err != nil {
		return 0, err
	}
	return record.Args[0].(int64), nil
}
