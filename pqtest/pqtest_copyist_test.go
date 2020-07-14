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
		 by BYTES, f FLOAT, d DECIMAL, fa FLOAT[], u UUID)
	`, nil}}
	r17 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`
		INSERT INTO datatypes VALUES
			(1, 'foo', '2000-01-01T10:00:00Z', '2000-01-01T10:00:00Z', true,
			 'ABCD', 1.1, 100.1234, ARRAY(1.1, 2.2), '8B78978B-7D8B-489E-8CA9-AC4BDC495A82'),
			(2, '', '2000-02-02T11:11:11-08:00', '2000-02-02T11:11:11-08:00', false,
			 '', -1e10, -0.0, ARRAY(), '00000000-0000-0000-0000-000000000000')
	`, nil}}
	r18 := &copyist.Record{copyist.ResultRowsAffected, copyist.RecordArgs{int64(0), nil}}
	r19 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT i, s, tz, t, b, by, f, d, fa, u FROM datatypes`, nil}}
	r20 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`i`, `s`, `tz`, `t`, `b`, `by`, `f`, `d`, `fa`, `u`}}}
	r21 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{int64(1), `foo`, copyist.ParseTime(`2000-01-01T10:00:00Z`), copyist.ParseTime(`2000-01-01T10:00:00+00:00`), true, copyist.ParseBase64(`QUJDRA`), 1.1, copyist.ParseBase64(`MTAwLjEyMzQ`), copyist.ParseBase64(`ezEuMSwyLjJ9`), copyist.ParseBase64(`OGI3ODk3OGItN2Q4Yi00ODllLThjYTktYWM0YmRjNDk1YTgy`)}, nil}}
	r22 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{int64(2), ``, copyist.ParseTime(`2000-02-02T19:11:11Z`), copyist.ParseTime(`2000-02-02T11:11:11+00:00`), false, copyist.ParseBase64(``), -1e+10, copyist.ParseBase64(`MC4w`), copyist.ParseBase64(`e30`), copyist.ParseBase64(`MDAwMDAwMDAtMDAwMC0wMDAwLTAwMDAtMDAwMDAwMDAwMDAw`)}, nil}}
	r23 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT 1::float, 1.1::float, 1e20::float`, nil}}
	r24 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`float8`, `float8`, `float8`}}}
	r25 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{1., 1.1, 1e+20}, nil}}
	r26 := &copyist.Record{copyist.ConnBegin, copyist.RecordArgs{nil}}
	r27 := &copyist.Record{copyist.TxCommit, copyist.RecordArgs{nil}}
	r28 := &copyist.Record{copyist.TxRollback, copyist.RecordArgs{nil}}
	r29 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SHOW session_id`, nil}}
	r30 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`session_id`}}}
	r31 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`16218617ca1391840000000000000001`}, nil}}
	r32 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`16218617d34b6c900000000000000001`}, nil}}
	r33 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT name FROM customers WHERE id=?`, nil}}
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestDataTypes`, copyist.Recording{r1, r16, r13, r10, r17, r13, r10, r18, r19, r13, r4, r20, r21, r22})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestFloatLiterals.func1`, copyist.Recording{r1, r23, r13, r4, r24, r25})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestFloatLiterals.func2`, copyist.Recording{r1, r23, r13, r4, r24, r25})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestIndirectOpen`, copyist.Recording{r1, r2, r3, r4, r5, r6})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestInsert`, copyist.Recording{r1, r8, r9, r10, r11, r12, r13, r4, r14, r15, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestPooling.func1`, copyist.Recording{r1, r29, r13, r4, r30, r31, r7, r29, r13, r4, r30, r31, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestPooling.func2`, copyist.Recording{r1, r29, r13, r4, r30, r32})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestQuery`, copyist.Recording{r1, r2, r3, r4, r5, r6, r7})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestSqlx`, copyist.Recording{r1, r26, r33, r3, r4, r5, r6, r7, r27})
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestTxns`, copyist.Recording{r1, r26, r8, r9, r10, r27, r26, r8, r9, r10, r28, r12, r13, r4, r14, r15, r7})
}
