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
	"database/sql/driver"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"strconv"
	"strings"
	"time"
)

// ParseTime is a helper used by generated copyist code to parse time strings.
func ParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// deepCopyValue makes a deep copy of the given value. It is used to ensure that
// recorded values are immutable, and will never be updated by the application
// or driver. One case where this can happen is with driver.Rows.Next, where the
// storage for output values can be reused across calls to Next.
//
// Every data type handled in this function must also be handled in the
// constructValueAst function. When support for a new driver is added to
// copyist, this function needs to be updated for any types that can be returned
// by that driver.
func deepCopyValue(val interface{}) interface{} {
	switch t := val.(type) {
	case int, int64, float64, string, bool, time.Time, error, nil:
		return t
	case []string:
		return append([]string{}, t...)
	case []uint8:
		return append([]uint8{}, t...)
	case []driver.Value:
		newValues := make([]driver.Value, len(t))
		for i := range t {
			newValues[i] = deepCopyValue(t[i])
		}
		return newValues
	default:
		panic(fmt.Sprintf("unsupported type: %T", t))
	}
}

// constructValueAst creates an AST expression that constructs an exact copy of
// the given value. It is used when generating code that constructs return
// values for methods like driver.Rows.Next.
//
// Every data type handled in this function must also be handled in the
// deepCopyValues function. When support for a new driver is added to copyist,
// this function needs to be updated for any types that can be returned by that
// driver.
func constructValueAst(val interface{}) ast.Expr {
	switch t := val.(type) {
	case int:
		return constructIntLiteral(t)
	case int64:
		return &ast.CallExpr{
			Fun:  &ast.Ident{Name: "int64"},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.FormatInt(t, 10)}},
		}
	case float64:
		return constructFloatLiteral(t)
	case string:
		return constructStringLiteral(t)
	case bool:
		if t {
			return &ast.Ident{Name: "true"}
		}
		return &ast.Ident{Name: "false"}
	case time.Time:
		s := t.Format(time.RFC3339Nano)

		// time.Format normalizes the +00:00 UTC timezone into "Z". This causes
		// the recorded output to differ from the "real" driver output. Use a
		// format that's round-trippable using copyist.ParseTime.
		if strings.HasSuffix(s, "Z") && t.Location() != time.UTC {
			s = s[:len(s)-1] + "+00:00"
		}

		return &ast.CallExpr{
			Fun:  constructQName("copyist", "ParseTime"),
			Args: []ast.Expr{constructStringLiteral(s)},
		}
	case []string:
		var elts []ast.Expr
		for _, s := range t {
			elts = append(elts, constructStringLiteral(s))
		}
		return &ast.CompositeLit{
			Type: &ast.ArrayType{Elt: &ast.Ident{Name: "string"}},
			Elts: elts,
		}
	case []uint8:
		var elts []ast.Expr
		for _, b := range t {
			elts = append(elts, constructIntLiteral(int(b)))
		}
		return &ast.CompositeLit{
			Type: &ast.ArrayType{Elt: &ast.Ident{Name: "uint8"}},
			Elts: elts,
		}
	case []driver.Value:
		var elts []ast.Expr
		for _, v := range t {
			elts = append(elts, constructValueAst(v))
		}
		return &ast.CompositeLit{
			Type: &ast.ArrayType{Elt: constructQName("driver", "Value")},
			Elts: elts,
		}
	case error:
		if t == io.EOF {
			return constructQName("io", "EOF")
		}
		return &ast.CallExpr{
			Fun:  constructQName("errors", "New"),
			Args: []ast.Expr{constructStringLiteral(t.Error())},
		}
	case nil:
		return &ast.Ident{Name: "nil"}
	default:
		panic(fmt.Sprintf("unsupported type: %T", t))
	}
}

func constructIntLiteral(val int) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(val)}
}

func constructFloatLiteral(val float64) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.FLOAT, Value: strconv.FormatFloat(val, 'g', 19, 64)}
}

func constructStringLiteral(val string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("`%s`", val)}
}
