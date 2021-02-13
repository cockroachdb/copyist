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

// proxyTx records and plays back calls to driver.Tx methods.
type proxyTx struct {
	// Tx is a transaction.
	driver.Tx

	tx driver.Tx
}

// Commit commits the transaction.
func (t *proxyTx) Commit() error {
	if IsRecording() {
		err := t.tx.Commit()
		currentSession.AddRecord(&record{Typ: TxCommit, Args: recordArgs{err}})
		return err
	}

	record := currentSession.VerifyRecord(TxCommit)
	err, _ := record.Args[0].(error)
	return err
}

// Rollback aborts the transaction.
func (t *proxyTx) Rollback() error {
	if IsRecording() {
		err := t.tx.Rollback()
		currentSession.AddRecord(&record{Typ: TxRollback, Args: recordArgs{err}})
		return err
	}

	record := currentSession.VerifyRecord(TxRollback)
	err, _ := record.Args[0].(error)
	return err
}
