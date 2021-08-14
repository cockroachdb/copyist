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
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/scanner"
	"time"

	"github.com/jackc/pgproto3"
	"github.com/lib/pq"
)

// valueType is an enumeration of all types that can be round-tripped to and
// from a copyist recording file, with no important information lost in the
// translation. If/when other drivers are supported, they will need to add
// support for any types that are not already handled:
//
//   1. Add enumeration value below. Use an explicit numeric value so that its
//      easier to look up a type by number. For a new driver, leave plenty of
//      space between numeric runs so that previous drivers can add more types.
//   2. Add a case to the formatValueWithType switch.
//   3. Add a case to the parseValueWithType switch.
//   4. Add a case to the deepCopyValue switch if the value's content might be
//      mutated across calls to the driver.
//
type valueType int

const (
	_               valueType = 0
	nilType         valueType = 1
	stringType      valueType = 2
	intType         valueType = 3
	int64Type       valueType = 4
	float64Type     valueType = 5
	boolType        valueType = 6
	errorType       valueType = 7
	timeType        valueType = 8
	stringSliceType valueType = 9
	byteSliceType   valueType = 10
	valueSliceType  valueType = 11

	// Custom pq types.
	pqErrorType valueType = 100
)

// formatValueWithType converts the given value into a formatted string suitable
// for inclusion in a copyist recording file. The format is as follows:
//
//   <dataType>:<formattedValue>
//
// where dataType is the numeric value of the `copyist.valueType` enumeration,
// and stringValue is a formatted representation of the value that conforms to
// these rules:
//
//   1. Contains no linefeed or newline characters. This allows simple
//      line-by-line parsing of record declarations.
//   2. Contains no tab characters. This allows a simple string split by the tab
//      character to break a line into fields.
//   3. Contains no bracket or comma characters, except as part of a valid Go
//      literal string format. This allows nested slice types to be parsed.
//
// Many data types already follow these rules with nothing more to do. Those
// data types that do not (e.g. string) need to perform escaping in order to
// ensure their formatted representation never contains disallowed characters.
func formatValueWithType(val interface{}) string {
	if val == nil {
		return fmt.Sprintf("%d:nil", nilType)
	}

	switch t := val.(type) {
	// Custom pq types.
	case *pq.Error:
		return fmt.Sprintf("%d:%s", pqErrorType, formatPqError(t))

	// Built-in Go types.
	case string:
		return fmt.Sprintf("%d:%s", stringType, strconv.Quote(t))
	case int:
		return fmt.Sprintf("%d:%d", intType, val)
	case int64:
		return fmt.Sprintf("%d:%d", int64Type, val)
	case float64:
		return fmt.Sprintf("%d:%g", float64Type, t)
	case bool:
		return fmt.Sprintf("%d:%v", boolType, t)
	case error:
		return fmt.Sprintf("%d:%s", errorType, strconv.Quote(t.Error()))
	case time.Time:
		// time.Format normalizes the +00:00 UTC timezone into "Z". This causes
		// the recorded output to differ from the "real" driver output. Use a
		// format that's round-trippable by parseValueWithType.
		s := t.Format(time.RFC3339Nano)
		if strings.HasSuffix(s, "Z") && t.Location() != time.UTC {
			s = s[:len(s)-1] + "+00:00"
		}
		return fmt.Sprintf("%d:%s", timeType, s)
	case []string:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, s := range t {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(strconv.Quote(s))
		}
		buf.WriteByte(']')
		return fmt.Sprintf("%d:%s", stringSliceType, buf.String())
	case []byte:
		s := base64.RawStdEncoding.EncodeToString(t)
		return fmt.Sprintf("%d:%s", byteSliceType, s)
	case []driver.Value:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, v := range t {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(formatValueWithType(v))
		}
		buf.WriteByte(']')
		return fmt.Sprintf("%d:%s", valueSliceType, buf.String())
	default:
		panic(fmt.Errorf("unsupported type: %T", t))
	}
}

// formatPqError returns a lib/pq error as a string that is suitable for
// inclusion in a copyist recording file. It does this by using the pgproto3
// library to format the error using the Postgres wire protocol, and then
// returns it as a quoted string.
func formatPqError(pqErr *pq.Error) string {
	resp := pgproto3.ErrorResponse{
		Severity:         pqErr.Severity,
		Code:             string(pqErr.Code),
		Message:          pqErr.Message,
		Detail:           pqErr.Detail,
		Hint:             pqErr.Hint,
		Position:         stringToInt32OrZero(pqErr.Position),
		InternalPosition: stringToInt32OrZero(pqErr.InternalPosition),
		InternalQuery:    pqErr.InternalQuery,
		Where:            pqErr.Where,
		SchemaName:       pqErr.Schema,
		TableName:        pqErr.Table,
		ColumnName:       pqErr.Column,
		DataTypeName:     pqErr.DataTypeName,
		ConstraintName:   pqErr.Constraint,
		File:             pqErr.File,
		Line:             stringToInt32OrZero(pqErr.Line),
		Routine:          pqErr.Routine,
	}

	// Encode using the pgproto3 library and skip the Error header bytes.
	encoded := resp.Encode(nil)
	encoded = encoded[5:]

	return strconv.Quote(string(encoded))
}

// parseValueWithType parses a value from the copyist recording file, in the
// format produced by the `formatValueWithType` function:
//
//   <dataType>:<formattedValue>
//
// Only well-known "valueType" data types are supported.
func parseValueWithType(valWithTyp string) (interface{}, error) {
	index := strings.Index(valWithTyp, ":")
	if index == -1 {
		return nil, errors.New("expected colon")
	}
	num, err := strconv.Atoi(valWithTyp[:index])
	if err != nil {
		return nil, err
	}
	typ := valueType(num)
	val := valWithTyp[index+1:]

	switch typ {
	// Custom pq types.
	case pqErrorType:
		return parsePqError(val)

	// Built-in Go types.
	case nilType:
		if val != "nil" {
			return nil, errors.New("expected nil")
		}
		return nil, nil
	case stringType:
		return strconv.Unquote(val)
	case intType:
		return strconv.Atoi(val)
	case int64Type:
		return strconv.ParseInt(val, 10, 64)
	case float64Type:
		return strconv.ParseFloat(val, 64)
	case boolType:
		if val == "false" {
			return false, nil
		} else if val == "true" {
			return true, nil
		}
		return nil, errors.New("expected true or false")
	case errorType:
		s, err := strconv.Unquote(val)
		if err != nil {
			return nil, err
		}
		if s == "EOF" {
			// Return reference to singleton object so that callers can compare
			// by reference.
			return io.EOF, nil
		}
		return errors.New(s), nil
	case timeType:
		return time.Parse(time.RFC3339Nano, val)
	case stringSliceType:
		strs, err := parseSlice(val)
		if err != nil {
			return nil, err
		}
		for i := range strs {
			strs[i], err = strconv.Unquote(strs[i])
			if err != nil {
				return nil, err
			}
		}
		return strs, nil
	case byteSliceType:
		return base64.RawStdEncoding.DecodeString(val)
	case valueSliceType:
		slice, err := parseSlice(val)
		if err != nil {
			return nil, err
		}
		valueSlice := make([]driver.Value, len(slice))
		for i := range slice {
			valueSlice[i], err = parseValueWithType(slice[i])
			if err != nil {
				return nil, err
			}
		}
		return valueSlice, nil
	default:
		panic(fmt.Errorf("unsupported type: %v", typ))
	}
}

// parsePqError parses a string value that was formatted by formatPqError. This
// is expected to be Postgres wire protocol bytes that encode a Postgres error,
// as a quoted string.
func parsePqError(val string) (interface{}, error) {
	unquoted, err := strconv.Unquote(val)
	if err != nil {
		return nil, err
	}

	var resp pgproto3.ErrorResponse
	if err = resp.Decode([]byte(unquoted)); err != nil {
		return nil, err
	}

	return &pq.Error{
		Severity:         resp.Severity,
		Code:             pq.ErrorCode(resp.Code),
		Message:          resp.Message,
		Detail:           resp.Detail,
		Hint:             resp.Hint,
		Position:         strconv.Itoa(int(resp.Position)),
		InternalPosition: strconv.Itoa(int(resp.InternalPosition)),
		InternalQuery:    resp.InternalQuery,
		Where:            resp.Where,
		Schema:           resp.SchemaName,
		Table:            resp.TableName,
		Column:           resp.ColumnName,
		DataTypeName:     resp.DataTypeName,
		Constraint:       resp.ConstraintName,
		File:             resp.File,
		Line:             strconv.Itoa(int(resp.Line)),
		Routine:          resp.Routine,
	}, nil
}

// deepCopyValue makes a deep copy of the given value. It is used to ensure that
// recorded values are immutable, and will never be updated by the application
// or driver. One case where this can happen is with driver.Rows.Next, where the
// storage for output values can be reused across calls to Next.
func deepCopyValue(val interface{}) interface{} {
	switch t := val.(type) {
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
		// Most types don't need special handling.
		return t
	}
}

// splitString is a wrapper around strings.Split that returns an empty slice in
// the case where the input string is empty. strings.Split returns a slice with
// one empty string instead.
func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, sep)
}

// parseSlice is a simple parser that handles nested slice declarations of the
// form:
//
//   ["foo", ["bar", 55], "baz"]
//
// It returns a slice of strings representing the "top-level" strings in the
// slice, equivalent to this:
//
//   []string{"foo", `["bar", 55]`, "baz"}
//
// Tokenization of the input string is done according to Golang rules.
func parseSlice(s string) ([]string, error) {
	// Trim leading and trailing brackets.
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("invalid slice format: %s", s)
	}
	s = s[1 : len(s)-1]
	if len(s) == 0 {
		// Slice is empty.
		return []string{}, nil
	}

	// Tokenize comma-delimited list.
	var buf bytes.Buffer
	var tokens []string
	var scan scanner.Scanner
	scan.Init(strings.NewReader(s))
	scan.Mode = scanner.ScanStrings

	nesting := 0
	for {
		tok := scan.Scan()
		switch tok {
		case ',':
			if nesting == 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
				continue
			}
		case '[':
			nesting++
		case ']':
			if nesting == 0 {
				return nil, fmt.Errorf("mismatched brackets: %s", s)
			}
			nesting--
		case scanner.EOF:
			if nesting != 0 {
				return nil, fmt.Errorf("mismatched brackets: %s", s)
			}
			tokens = append(tokens, buf.String())
			return tokens, nil
		}

		buf.WriteString(scan.TokenText())
	}
}

// stringToInt32OrZero converts the given string into an int32 value, or returns
// zero if it cannot (typically when string is empty).
func stringToInt32OrZero(val string) int32 {
	pos, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0
	}
	return int32(pos)
}
