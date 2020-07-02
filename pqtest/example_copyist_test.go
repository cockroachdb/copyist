package pqtest_test

import (
	"database/sql/driver"
	"errors"
	"io"
	"time"

	"github.com/cockroachdb/copyist"
)

var _ = driver.ErrBadConn
var _ = io.EOF
var _ = time.Parse
var _ = copyist.Register
var _ = errors.New

func init() {
	copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestQueryName`, []copyist.Record{{Typ: copyist.DriverOpen, Args: copyist.RecordArgs{nil}}, {Typ: copyist.ConnPrepare, Args: copyist.RecordArgs{`SELECT name FROM customers WHERE id=$1`, nil}}, {Typ: copyist.StmtNumInput, Args: copyist.RecordArgs{1}}, {Typ: copyist.StmtQuery, Args: copyist.RecordArgs{nil}}, {Typ: copyist.RowsColumns, Args: copyist.RecordArgs{[]string{`name`}}}, {Typ: copyist.RowsNext, Args: copyist.RecordArgs{[]driver.Value{`Andy`}, nil}}})

}
