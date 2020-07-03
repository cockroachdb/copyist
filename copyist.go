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

import (
	"bytes"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"strings"
)

// record instructs copyist to record all calls to the registered driver, if
// true. Otherwise, it plays back previously recorded calls.
var record = flag.Bool("record", true, "record sql database accesses")

var visitedRecording bool

// IsRecording returns true if copyist is currently in recording mode.
func IsRecording() bool {
	// Determine whether the "record" flag was explicitly passed rather than
	// defaulted. This is painful and slow in Go, so do it just once.
	if !visitedRecording {
		found := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "record" {
				found = true
			}
		})
		if !found {
			*record = false
		}
		visitedRecording = true
	}
	return *record
}

// ResetCallback types a function that is invoked during each call to
// copyist.Open, when in recording mode, in order to reset the database to a
// clean, well-known state.
type ResetCallback func()

// closer implements the io.Closer interface by invoking an arbitrary function
// when Close is called.
type closer func() error

func (c closer) Close() error {
	return c()
}

// testOut is used by tests to verify the output recording. If testOut is not
// nil, then the recording will be sent to this writer rather than to a file.
var testOut io.Writer

// registered is the proxy driver created during the registration process. It is
// nil if Register has not yet been called.
var registered *proxyDriver

// recordingMap maps from driverName + funcName to the recording made for that
// combination.
var recordingMap = make(map[string][]Record)

// AddRecording is called by the generated code to enter all the recordings into
// the map as part of initialization. Those recordings can then be accessed for
// playback.
func AddRecording(recordingName string, recording []Record) {
	recordingMap[recordingName] = recording
}

// Register constructs a proxy driver that wraps the "real" driver of the given
// name. Depending on the value of the "record" command-line flag, the
// constructed proxy will either record calls to the wrapped driver, or else
// play back calls that were previously recorded. Register must be called before
// Open can be called, typically in a TestMain method or equivalent.
//
// Register method takes the name of the SQL driver to be wrapped (e.g.
// "postgres"). The resetDB function (if defined) resets the database to a
// clean, well-known state. It is only called in "recording" mode, each time
// that copyist.Open is called by a test. There is no need to call it in
// "playback" mode, as the database is not actually accessed at that time.
//
// Below is an example of how copyist.Register should be invoked.
//
//   err := copyist.Register("postgres", resetDB)
//
// Note that Register can only be called once; subsequent attempts will fail
// with an error. In addition, the same driver must be used with playback as was
// was used during recording.
func Register(driverName string, resetDB ResetCallback) error {
	if registered != nil {
		return errors.New("Register cannot be called more than once")
	}

	// Get the "real" driver that will be wrapped. Unfortunately, the sql
	// package does not offer any good way to do this. Calling Open and then
	// getting the driver from the DB object is a hacky workaround.
	// TODO(andyk): any better way to do this? Calling Open will fail for
	// drivers that immediately open a connection rather than doing it lazily.
	db, err := sql.Open(driverName, "")
	if err != nil {
		return err
	}
	wrapped := db.Driver()
	db.Close()

	if IsRecording() {
		registered = &proxyDriver{resetDB: resetDB, wrapped: wrapped, driverName: driverName}
	} else {
		registered = &proxyDriver{driverName: driverName}
	}
	sql.Register("copyist_"+driverName, registered)

	return nil
}

// Open begins a recording or playback session, depending on the value of the
// "record" command-line flag. If recording, then all calls to the registered
// driver will be recorded and then saved as Go code in a generated file that
// sits alongside the calling test file. If playing back, then the recording
// will be fetched from that generated file. Here is a typical calling pattern:
//
//   func TestMain(m *testing.M) {
//     flag.Parse()
//     copyist.Register("postgres")
//     os.Exit(m.Run())
//   }
//
//   func TestMyStuff(t *testing.T) {
//     defer copyist.Open().Close()
//     ...
//   }
//
// The call to Open will initiate a new recording session. The deferred call to
// Close will complete the recording session and write the recording to a file
// alongside the test file, such as:
//
//   mystuff_test.go
//   mystuff_copyist_test.go
//
// Each test (or sub-test) should record its own session so that they can be
// executed independently.
func Open() io.Closer {
	if registered == nil {
		panic("Register was not called")
	}

	// Get name and path of calling test function.
	pc, fileName, _, _ := runtime.Caller(1)
	if !strings.HasSuffix(fileName, "_test.go") {
		panic(fmt.Sprintf("Open was not called from a test file: %v", fileName))
	}

	// Extract package name from calling function name.
	funcName := runtime.FuncForPC(pc).Name()

	// Synthesize the recording name by prepending the driver name.
	recordingName := fmt.Sprintf("%s/%s", registered.driverName, funcName)

	if IsRecording() {
		// Construct the recording file name by prefixing the "_test" suffix
		// with "_copyist".
		fileName = fileName[:len(fileName)-8] + "_copyist_test.go"

		return openForRecording(recordingName, fileName)
	}
	return openForPlayback(recordingName)
}

func openForRecording(recordingName, fileName string) io.Closer {
	// Invoke resetDB callback, if defined.
	if registered.resetDB != nil {
		registered.resetDB()
	}

	// Reset recording (including any recording that occurred during the
	// database reset).
	registered.recording = []Record{}
	registered.index = 0

	// If recording file has not yet been created, do so now.
	ensureRecordingFile(recordingName, fileName)

	// Parse the file as Go code and produce an AST.
	fset := token.NewFileSet()
	sqlAst, err := parser.ParseFile(fset, fileName, nil, 0)
	if err != nil {
		panic(fmt.Sprintf("error parsing sql file: %v", err))
	}

	// Look for an existing AddRecording call in the init method, and remove it,
	// since it will be replaced with this new recording.
	initFn := updateInit(sqlAst, recordingName)
	if initFn == nil {
		panic(fmt.Sprintf("init function could not be found in recording file: %s", fileName))
	}

	// Once the recording session has been closed, construct a new AddRecording
	// call and add it to the body of the init function.
	return closer(func() error {
		// Construct the new AddRecording call and add it to the init function's
		// body.
		addRecordingCall := &ast.ExprStmt{X: &ast.CallExpr{
			Fun: constructQName("copyist", "AddRecording"),
			Args: []ast.Expr{
				constructStringLiteral(recordingName),
				constructRecordingAst(registered.recording),
			},
		}}
		initFn.Body.List = append(initFn.Body.List, addRecordingCall)

		// Format the AST as Go code. Write to buffer first, since errors would
		// otherwise cause WriteFile to clear the file.
		var buf bytes.Buffer
		if err = format.Node(&buf, fset, sqlAst); err != nil {
			panic(fmt.Sprintf("error printing sql AST: %v", err))
		}

		// If testOut is defined, redirect output there instead of file.
		if testOut != nil {
			testOut.Write(buf.Bytes())
		} else {
			err := ioutil.WriteFile(fileName, buf.Bytes(), 0666)
			if err != nil {
				panic(fmt.Sprintf("error writing modified sql file: %v", err))
			}
		}

		registered.recording = nil

		return nil
	})
}

func openForPlayback(recordingName string) io.Closer {
	recording, ok := recordingMap[recordingName]
	if !ok {
		panic(fmt.Sprintf("no recording exists with this name: %v", recordingName))
	}

	// Reset the registered driver with the recording to play back.
	registered.recording = recording
	registered.index = 0

	// Close is a no-op for playback.
	return closer(func() error {
		registered.recording = nil
		registered.index = 0
		return nil
	})
}

// ensureRecordingFile creates a recording file if one does not yet exist and
// writes a "skeleton" init function into that file.
func ensureRecordingFile(recordingName, fileName string) {
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		// It's very difficult to determine if a particular import is used, so
		// just include them all.
		pkgName := extractPackageName(recordingName)
		goCode := fmt.Sprintf(initSkeleton, pkgName)
		err = ioutil.WriteFile(fileName, []byte(goCode), 0666)
		if err != nil {
			panic(fmt.Sprintf("error writing initial sql file: %v", err))
		}
	}
}

// updateInit looks for an init method in the given AST, and then, within that
// method, for an AddRecording call having a matching recording name as its
// first argument. updateInit removes that method and returns the init function
// it found. This is the pattern:
//
//   func init() {
//     copyist.AddRecording("<recordingName1>", []copyist.Record{...})
//     copyist.AddRecording("<recordingName2>", []copyist.Record{...})
//     ...
//   }
//
func updateInit(sqlAst *ast.File, recordingName string) *ast.FuncDecl {
	var initFn *ast.FuncDecl
	for _, decl := range sqlAst.Decls {
		// Find init function.
		var ok bool
		initFn, ok = decl.(*ast.FuncDecl)
		if !ok || initFn.Name.Name != "init" {
			continue
		}

		// Find AddRecording call with first parameter that matches
		// recordingName.
		quotedRecordingName := fmt.Sprintf("`%s`", recordingName)
		for i, stmt := range initFn.Body.List {
			expr, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			addCall, ok := expr.X.(*ast.CallExpr)
			if !ok || len(addCall.Args) != 2 {
				continue
			}

			lit, ok := addCall.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || lit.Value != quotedRecordingName {
				continue
			}

			// Remove AddRecording call from list.
			initFn.Body.List = append(initFn.Body.List[:i], initFn.Body.List[i+1:]...)
			break
		}

		break
	}

	return initFn
}

// constructRecordingAst creates an AST expression that constructs the given
// recording as a Go literal. For example:
//
//   []copyist.Record{{Typ: copyist.DriverOpen, Args: copyist.RecordArgs{...}}}
//
func constructRecordingAst(recording []Record) ast.Expr {
	// []copyist.Record{}
	recordingAst := &ast.CompositeLit{
		Type: &ast.ArrayType{Elt: constructQName("copyist", "Record")},
	}

	// Construct AST expression for each record in the list.
	for _, record := range recording {
		var args []ast.Expr
		for _, arg := range record.Args {
			args = append(args, constructValueAst(arg))
		}

		// {Typ: "copyist.RecordType", Args: copyist.RecordArgs{...}}
		recordAst := &ast.CompositeLit{Elts: []ast.Expr{
			&ast.KeyValueExpr{
				Key:   &ast.Ident{Name: "Typ"},
				Value: constructQName("copyist", record.Typ.String()),
			},
			&ast.KeyValueExpr{
				Key: &ast.Ident{Name: "Args"},
				Value: &ast.CompositeLit{
					Type: constructQName("copyist", "RecordArgs"),
					Elts: args,
				},
			},
		}}

		recordingAst.Elts = append(recordingAst.Elts, recordAst)
	}
	return recordingAst
}

func constructQName(qualifier, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: &ast.Ident{Name: qualifier}, Sel: &ast.Ident{Name: name}}
}

// extractPackageName returns the package component of a function name returned
// by FuncForPC in this format:
//
//   github.com/cockroachlabs/managed-service/copyist/cmd.TestFoo.func1
//
// The package name is the last component in the "/" path.
func extractPackageName(funcName string) string {
	start := strings.LastIndex(funcName, "/")
	pkgName := funcName[start+1:]
	end := strings.Index(pkgName, ".")
	return pkgName[:end]
}

var initSkeleton = `package %s

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

func init() {}
`
