package pqtest_test

import (
	"github.com/cockroachdb/copyist"
	"database/sql/driver"
)

func init() {
	r1 := &copyist.Record{copyist.DriverOpen, copyist.RecordArgs{nil}}
	r2 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT name FROM customers WHERE id=$1`, nil}}
	r3 := &copyist.Record{copyist.StmtNumInput, copyist.RecordArgs{1}}
	r4 := &copyist.Record{copyist.StmtQuery, copyist.RecordArgs{nil}}
	r5 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`name`}}}
	r6 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`Andy`}, nil}}
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestQueryName`, copyist.Recording{r1, r2, r3, r4, r5, r6})
}
