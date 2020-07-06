package pqtest_test

import (
	"github.com/cockroachdb/copyist"
	"database/sql/driver"
	"io"
)

func init() {
	r1 := &copyist.Record{copyist.DriverOpen, copyist.RecordArgs{nil}}
	r2 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT name FROM customers WHERE id=$1`, nil}}
	r3 := &copyist.Record{copyist.StmtNumInput, copyist.RecordArgs{1}}
	r4 := &copyist.Record{copyist.StmtQuery, copyist.RecordArgs{nil}}
	r5 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`name`}}}
	r6 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`Andy`}, nil}}
	r7 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{}, io.EOF}}
	r8 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`INSERT INTO customers VALUES ($1, $2)`, nil}}
	r9 := &copyist.Record{copyist.StmtNumInput, copyist.RecordArgs{2}}
	r10 := &copyist.Record{copyist.StmtExec, copyist.RecordArgs{nil}}
	r11 := &copyist.Record{copyist.ResultRowsAffected, copyist.RecordArgs{int64(1), nil}}
	r12 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT COUNT(*) FROM customers`, nil}}
	r13 := &copyist.Record{copyist.StmtNumInput, copyist.RecordArgs{0}}
	r14 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`count`}}}
	r15 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{int64(4)}, nil}}
	r16 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`
		CREATE TABLE datatypes
		(i INT, s TEXT, tz TIMESTAMPTZ, t TIMESTAMP, b BOOL,
		 by BYTES, f FLOAT, d DECIMAL, fa FLOAT[])
	`, nil}}
	r17 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`
		INSERT INTO datatypes VALUES
			(1, 'foo', '2000-01-01T10:00:00Z', '2000-01-01T10:00:00Z', true,
			 'ABCD', 1.1, 100.1234, ARRAY(1.1, 2.2)),
			(2, '', '2000-02-02T11:11:11-08:00', '2000-02-02T11:11:11-08:00', false,
			 '', -1e10, -0.0, ARRAY())
	`, nil}}
	r18 := &copyist.Record{copyist.ResultRowsAffected, copyist.RecordArgs{int64(0), nil}}
	r19 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT i, s, tz, t, b, by, f, d, fa FROM datatypes`, nil}}
	r20 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`i`, `s`, `tz`, `t`, `b`, `by`, `f`, `d`, `fa`}}}
	r21 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{int64(1), `foo`, copyist.ParseTime(`2000-01-01T10:00:00Z`), copyist.ParseTime(`2000-01-01T10:00:00+00:00`), true, []uint8{65, 66, 67, 68}, 1.100000000000000089, []uint8{49, 48, 48, 46, 49, 50, 51, 52}, []uint8{123, 49, 46, 49, 44, 50, 46, 50, 125}}, nil}}
	r22 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{int64(2), ``, copyist.ParseTime(`2000-02-02T19:11:11Z`), copyist.ParseTime(`2000-02-02T11:11:11+00:00`), false, []uint8{}, -10000000000, []uint8{48, 46, 48}, []uint8{123, 125}}, nil}}
	r23 := &copyist.Record{copyist.ConnBegin, copyist.RecordArgs{nil}}
	r24 := &copyist.Record{copyist.TxCommit, copyist.RecordArgs{nil}}
	r25 := &copyist.Record{copyist.TxRollback, copyist.RecordArgs{nil}}
	r26 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SHOW session_id`, nil}}
	r27 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`session_id`}}}
	r28 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`161f10b29f3c34340000000000000001`}, nil}}
	r29 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`161f10b2a55e02840000000000000001`}, nil}}
	r30 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT name FROM customers WHERE id=?`, nil}}
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestDataTypes`, copyist.Recording{r1, r16, r13, r10, r17, r13, r10, r18, r19, r13, r4, r20, r21, r22})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestIndirectOpen`, copyist.Recording{r1, r2, r3, r4, r5, r6})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestInsert`, copyist.Recording{r1, r8, r9, r10, r11, r12, r13, r4, r14, r15, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestPooling.func1`, copyist.Recording{r1, r26, r13, r4, r27, r28, r7, r26, r13, r4, r27, r28, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestPooling.func2`, copyist.Recording{r1, r26, r13, r4, r27, r29})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestQuery`, copyist.Recording{r1, r2, r3, r4, r5, r6, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestSqlx`, copyist.Recording{r1, r23, r30, r3, r4, r5, r6, r7, r24})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestTxns`, copyist.Recording{r1, r23, r8, r9, r10, r24, r23, r8, r9, r10, r25, r12, r13, r4, r14, r15, r7})
}
