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
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
)

// recordingGen is a helper struct that creates and modifies recording test
// files using the Go AST library. If a test file already exists, then it will
// parse that file in order to merge in the new recording. Multiple recordings
// share record declarations, since there is often a great deal of redundancy
// across recordings. Here is a simplified example of a generated test file:
//
//   func init() {
//     r1 := &copyist.Record{copyist.DriverOpen, copyist.RecordArgs{nil}}
//     r2 := &copyist.Record{copyist.ConnPrepare, copyist.RecordArgs{`SELECT name FROM customers WHERE id=$1`, nil}}
//     r3 := &copyist.Record{copyist.StmtQuery, copyist.RecordArgs{nil}}
//     r4 := &copyist.Record{copyist.RowsColumns, copyist.RecordArgs{[]string{`name`}}}
//     r5 := &copyist.Record{copyist.RowsNext, copyist.RecordArgs{[]driver.Value{`Andy`}, nil}}
//     copyist.AddRecording(`postgres/github.com/cockroachdb/copyist/pqtest_test.TestQueryName`, copyist.Recording{r1, r2, r3, r4, r5})
//   }
//
// The "r?" variable declarations are called "record declarations", and they
// construct a single playback record. The record declarations are shared by the
// AddRecording calls.
type recordingGen struct {
	// fileName is the name of the copyist test file.
	fileName string

	// recordingName is the driver + full test name. It uniquely identifies a
	// particular recording across an entire project.
	recordingName string

	// recording is the copyist recording that will be inserted into the copyist
	// test file.
	recording Recording

	// hasher is used to generate MD5 hashes in order to determine when two
	// identical records can be shared.
	hasher astHasher

	// addRecordings is a map from recordingName to the copyist.AddRecording
	// AST statement. When a recording is re-generated, the old recording of the
	// same name will be replaced in the map.
	addRecordings map[string]*ast.ExprStmt

	// recordDecls is a map from the MD5 hash value of a record construction AST
	// expression to its assignment expression. Note that the hash value is not
	// computed over the AssignStmt, but instead over its r-value. This map
	// enables record declarations to be shared.
	recordDecls map[hashValue]*ast.AssignStmt

	// freeNums gives a list of record declaration numbers that are not in use
	// by any recording. This can happen when a test is changed in a way that no
	// longer requires some record. This causes a numbering "hole" in the
	// generated code (e.g. declarations skip from "r2" to "r4", with no "r3").
	// The numbering hole can be reused by a future recording.
	freeNums []int

	// nextNum gives the next "safe" record declaration number that can be used
	// for a new record. nextNum must always be greater than the largest number
	// in use by any record declaration. If "r5" is the last declaration, then
	// nextNum would be at least "r6".
	nextNum int
}

// generateRecordingFile creates and/or modifies a copyist test recording file
// of the given name. It merges the given recording into that file. See the
// comment header for recordingGen for more details.
func generateRecordingFile(recording Recording, recordingName, fileName string) {
	gen := recordingGen{
		fileName:      fileName,
		recordingName: recordingName,
		recording:     recording,
		addRecordings: make(map[string]*ast.ExprStmt),
		recordDecls:   make(map[hashValue]*ast.AssignStmt),
		nextNum:       1,
	}

	// Read record declarations and AddRecording calls from the copyist test
	// file, it it exists.
	gen.analyzeExistingRecordingFile()

	// Generate the new recording test file, merging the new recording into it.
	gen.constructNewRecordingFile()
}

// analyzeExistingRecordingFile reads record declarations and AddRecording calls
// from the copyist test file, if one exists. This information will later be
// merged with the record declarations and AddRecording call needed by the new
// recording.
func (g *recordingGen) analyzeExistingRecordingFile() {
	existingAst := readAstFromFile(g.fileName)
	if existingAst == nil {
		return
	}

	for _, decl := range existingAst.Decls {
		// Find init function.
		initFn, ok := decl.(*ast.FuncDecl)
		if !ok || initFn.Name.Name != "init" {
			continue
		}

		// Load record declarations and AddRecording calls into recordingGen
		// data structures.
		for _, stmt := range initFn.Body.List {
			switch t := stmt.(type) {
			case *ast.AssignStmt:
				// Record declaration. Determine any unused record declaration
				// names so they can be reused.
				declName := t.Lhs[0].(*ast.Ident).Name
				declNum, err := strconv.Atoi(declName[1:])
				if err != nil {
					panic(fmt.Sprintf("invalid assignment format: %v", declName))
				}
				if declNum < g.nextNum {
					panic("record declarations not sorted")
				}

				// Hash the record declaration.
				hash := g.hasher.HashAstNode(t.Rhs[0])
				if existing, ok := g.recordDecls[hash]; ok {
					panic(fmt.Sprintf("record %s should not have the same hash as %s",
						declName, existing.Lhs[0].(*ast.Ident).Name))
				}
				g.recordDecls[hash] = t

				// Update the freeNums list with any unused record declaration
				// numbers so they can be reused.
				for g.nextNum < declNum {
					g.freeNums = append(g.freeNums, g.nextNum)
					g.nextNum++
				}
				g.nextNum++

			case *ast.ExprStmt:
				// AddRecording call.
				if call, ok := t.X.(*ast.CallExpr); ok {
					if !ok || len(call.Args) != 2 {
						continue
					}

					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}

					// Trim leading and trailing quotes and add the call to the
					// addRecordings map.
					name := lit.Value[1 : len(lit.Value)-1]
					g.addRecordings[name] = t
				}
			}
		}
	}
}

// constructNewRecordingFile merges the new recording into the data structures
// created from the existing recording test file (if one existed). It then
// writes out the merged recordings to a new recording test file.
func (g *recordingGen) constructNewRecordingFile() {
	// Construct the AST nodes for the new recording and merge them into the
	// recordingGen data structures.
	g.constructNewRecording()

	// Construct AST file node, which will be the root of the tree.
	file := &ast.File{
		Name: &ast.Ident{Name: extractPackageName(g.recordingName)},
	}

	// Construct top-level init function.
	initFn := &ast.FuncDecl{
		Name: &ast.Ident{Name: "init"},
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{},
	}

	// Determine which record declarations are actually used, and then generate
	// those declarations in the init body.
	usedDecls := g.findUsedRecordDecls()
	for _, assign := range usedDecls {
		initFn.Body.List = append(initFn.Body.List, assign)
	}

	// Add sorted list of AddRecording calls.
	addRecordingCalls := g.sortAddRecordingCalls()
	initFn.Body.List = append(initFn.Body.List, addRecordingCalls...)

	// Construct top-level imports based on an analysis of what's needed by the
	// record declarations.
	var helper importAnalyzer
	imports := helper.FindNeededImports(usedDecls)
	if imports != nil {
		file.Decls = append(file.Decls, &ast.GenDecl{Tok: token.IMPORT, Specs: helper.imports})
	}
	file.Decls = append(file.Decls, initFn)

	// Write out AST as copyist test file.
	writeAstToFile(file, g.fileName)
}

// constructNewRecording constructs record declaration AST(s) and the
// AddRecording call AST for the new recording:
//
//   r1 := []copyist.Record{{copyist.DriverOpen, copyist.RecordArgs{...}}}
//
//   copyist.AddRecording(`...TestName`, copyist.Recording{r1})
//
// It merges the ASTs into the recordDecls and addRecordings data structures
// for later output.
func (g *recordingGen) constructNewRecording() {
	recordingAst := &ast.CompositeLit{
		Type: constructQName("copyist", "Recording"),
	}

	// Ensure that a record declaration exists for each record in the list.
	for _, record := range g.recording {
		// &copyist.Record{copyist.RecordType, copyist.RecordArgs{...}}
		var args []ast.Expr
		for _, arg := range record.Args {
			args = append(args, constructValueAst(arg))
		}
		recordAst := &ast.UnaryExpr{
			Op: token.AND,
			X: &ast.CompositeLit{
				Type: constructQName("copyist", "Record"),
				Elts: []ast.Expr{
					constructQName("copyist", record.Typ.String()),
					&ast.CompositeLit{
						Type: constructQName("copyist", "RecordArgs"),
						Elts: args,
					},
				},
			},
		}

		// Determine whether an identical record declaration already exists,
		// perhaps from another recording, or even from a previous step in this
		// same recording.
		hash := g.hasher.HashAstNode(recordAst)
		assign, ok := g.recordDecls[hash]
		if !ok {
			// No declaration yet exists for this record, so create it now.
			// Get record declaration number that is in use by any other
			// declaration.
			var declNum int
			if len(g.freeNums) != 0 {
				// Reuse declaration number that's now free.
				declNum = g.freeNums[0]
				g.freeNums = g.freeNums[1:]
			} else {
				// Use new declaration number.
				declNum = g.nextNum
				g.nextNum++
			}

			// Construct the record declaration and enter it into the
			// recordDecls map for reuse.
			declName := fmt.Sprintf("r%d", declNum)
			assign = &ast.AssignStmt{
				Lhs: []ast.Expr{&ast.Ident{Name: declName}},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{recordAst},
			}
			g.recordDecls[hash] = assign
		}

		recordingAst.Elts = append(recordingAst.Elts, assign.Lhs[0].(*ast.Ident))
	}

	// Construct the AddRecording call and enter it into the addRecordings map.
	// This may overwrite a previous recording for the same test.
	g.addRecordings[g.recordingName] = &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: constructQName("copyist", "AddRecording"),
			Args: []ast.Expr{
				constructStringLiteral(g.recordingName),
				recordingAst,
			},
		},
	}
}

// findUsedRecordDecls performs an analysis on the AddRecording calls in order
// to determine what set of record declarations they use. It returns a list of
// the needed record declarations.
func (g *recordingGen) findUsedRecordDecls() []*ast.AssignStmt {
	// Create record declaration lookup by variable name.
	recordDeclsByName := make(map[string]*ast.AssignStmt, len(g.recordDecls))
	for _, assign := range g.recordDecls {
		recordDeclsByName[assign.Lhs[0].(*ast.Ident).Name] = assign
	}

	// At most g.nextNum record declarations can exist, but there may be "holes"
	// where a particular declaration number is not in use. Those holes will be
	// eliminated afterwards.
	usedDecls := make([]*ast.AssignStmt, g.nextNum)

	for _, e := range g.addRecordings {
		recording := e.X.(*ast.CallExpr).Args[1].(*ast.CompositeLit)
		for _, elt := range recording.Elts {
			argName := elt.(*ast.Ident).Name
			argNum, err := strconv.Atoi(argName[1:])
			if err != nil {
				panic(fmt.Sprintf("invalid reference to record: %s", argName))
			}
			usedDecls[argNum-1] = recordDeclsByName[argName]
		}
	}

	// Consolidate the usedDecls list to account for any "holes" in the
	// numbering.
	to := 0
	for _, stmt := range usedDecls {
		if stmt != nil {
			usedDecls[to] = stmt
			to++
		}
	}
	usedDecls = usedDecls[:to]
	return usedDecls
}

// sortAddRecordingCalls returns a list of AddRecording call statements, sorted
// by recording name.
func (g *recordingGen) sortAddRecordingCalls() []ast.Stmt {
	names := make([]string, 0, len(g.addRecordings))
	for name := range g.addRecordings {
		names = append(names, name)
	}
	sort.Strings(names)

	stmts := make([]ast.Stmt, len(names))
	for i, name := range names {
		stmts[i] = g.addRecordings[name]
	}
	return stmts
}

// importAnalyzer analyzes the record declarations in order to determine what
// set of imports will be needed by the generated code.
type importAnalyzer struct {
	imports []ast.Spec
	seen    map[string]struct{}
}

// FindNeededImports walks the given record declarations and determines the set
// of imports that will be needed in the generated code.
func (d *importAnalyzer) FindNeededImports(recordDecls []*ast.AssignStmt) []ast.Spec {
	// Always add copyist import.
	d.imports = append(d.imports, &ast.ImportSpec{
		Path: constructStringLiteral("github.com/cockroachdb/copyist"),
	})

	for _, assign := range recordDecls {
		ast.Walk(d, assign)
	}

	return d.imports
}

// Visit implements the ast.Visitor interface.
func (d *importAnalyzer) Visit(node ast.Node) (w ast.Visitor) {
	switch t := node.(type) {
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			var importPath string
			switch ident.Name {
			case "driver":
				importPath = "database/sql/driver"
			case "errors":
				importPath = "errors"
			case "io":
				importPath = "io"
			case "time":
				importPath = "time"
			default:
				return d
			}

			// Use the seen map to ensure that the same import isn't added more
			// than once.
			if d.seen == nil {
				d.seen = make(map[string]struct{})
			}
			if _, ok := d.seen[importPath]; ok {
				// Import has already been seen, so don't add it again.
				return nil
			}
			d.seen[importPath] = struct{}{}

			d.imports = append(d.imports, &ast.ImportSpec{
				Path: constructStringLiteral(importPath),
			})
			return nil
		}
	}
	return d
}

// readAstFromFile parses the file of the given name as a Go AST.
func readAstFromFile(fileName string) *ast.File {
	// If no recording file yet exists, then nothing more to do.
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		return nil
	}

	ast, err := parser.ParseFile(token.NewFileSet(), fileName, nil, 0)
	if err != nil {
		panic(fmt.Sprintf("error parsing copyist-generated sql file: %v", err))
	}
	return ast
}

// writeAstToFile writes the given Go AST to a file of the given name.
func writeAstToFile(ast *ast.File, fileName string) {
	// Format the AST as Go code. Write to buffer first, since errors would
	// otherwise cause WriteFile to clear the file.
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), ast); err != nil {
		panic(fmt.Sprintf("error printing sql AST: %v", err))
	}

	err := ioutil.WriteFile(fileName, buf.Bytes(), 0666)
	if err != nil {
		panic(fmt.Sprintf("error writing modified sql file: %v", err))
	}
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

func constructQName(qualifier, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: &ast.Ident{Name: qualifier}, Sel: &ast.Ident{Name: name}}
}
