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

//go:generate stringer -type=recordType

package copyist

// recordType identifies the SQL driver method that was called during the
// recording process. It is stored as part of the Record struct, and is checked
// during playback.
type recordType int32

// This is a list of the event types, which correspond 1:1 with SQL driver
// methods.
const (
	_ recordType = iota
	DriverOpen
	ConnPrepare
	ConnBegin
	StmtNumInput
	StmtExec
	StmtQuery
	TxCommit
	TxRollback
	ResultLastInsertId
	ResultRowsAffected
	RowsColumns
	RowsNext
	_lastRecord = RowsNext
)

// strToRecType maps to a recordType value from its string representation.
var strToRecType map[string]recordType

func init() {
	strToRecType = make(map[string]recordType)
	for typ := recordType(1); typ <= _lastRecord; typ++ {
		strToRecType[typ.String()] = typ
	}
}
