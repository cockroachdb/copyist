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

package values

import (
	"bytes"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"text/scanner"
	"time"
)

// Type is an enumeration of all types that can be round-tripped to and from a
// copyist recording file, with no important information lost in the
// translation. If/when other drivers are supported, they will need to add
// support for any types that are not already handled:
//
//   1. Add enumeration value below. Use an explicit numeric value so that its
//      easier to look up a type by number. For a new driver, leave plenty of
//      space between numeric runs so that previous drivers can add more types.
//   2. Create a sub-package under the copyist package that implements custom
//      support and can be imported independently of other sub-packages.
//   3. In sub-package init function, add formatters for any custom value types
//      to Formatters.
//   4. In sub-package init function, add parsers for any custom value types to
//      Parsers.
//
type Type int

const (
	// Standard sql package value types.
	_               Type = 0
	NilType         Type = 1
	StringType      Type = 2
	IntType         Type = 3
	Int64Type       Type = 4
	Float64Type     Type = 5
	BoolType        Type = 6
	ErrorType       Type = 7
	TimeType        Type = 8
	StringSliceType Type = 9
	ByteSliceType   Type = 10
	ValueSliceType  Type = 11

	// Custom pq types.
	PqErrorType Type = 100
)

// Formatters
var Formatters map[reflect.Type]func(val interface{}) string
var Parsers map[Type]func(val string) (interface{}, error)

func init() {
	// Define formatters for standard sql package value types.
	Formatters = make(map[reflect.Type]func(val interface{}) string)

	Formatters[reflect.TypeOf("")] = func(val interface{}) string {
		return fmt.Sprintf("%d:%s", StringType, strconv.Quote(val.(string)))
	}
	Formatters[reflect.TypeOf(0)] = func(val interface{}) string {
		return fmt.Sprintf("%d:%d", IntType, val)
	}
	Formatters[reflect.TypeOf(int64(0))] = func(val interface{}) string {
		return fmt.Sprintf("%d:%d", Int64Type, val)
	}
	Formatters[reflect.TypeOf(float64(0))] = func(val interface{}) string {
		return fmt.Sprintf("%d:%g", Float64Type, val)
	}
	Formatters[reflect.TypeOf(false)] = func(val interface{}) string {
		return fmt.Sprintf("%d:%v", BoolType, val)
	}
	Formatters[reflect.TypeOf(false)] = func(val interface{}) string {
		return fmt.Sprintf("%d:%v", BoolType, val)
	}
	Formatters[reflect.TypeOf(time.Time{})] = func(val interface{}) string {
		// time.Format normalizes the +00:00 UTC timezone into "Z". This causes
		// the recorded output to differ from the "real" driver output. Use a
		// format that's round-trippable by ParseWithType.
		tm := val.(time.Time)
		s := tm.Format(time.RFC3339Nano)
		if strings.HasSuffix(s, "Z") && tm.Location() != time.UTC {
			s = s[:len(s)-1] + "+00:00"
		}
		return fmt.Sprintf("%d:%s", TimeType, s)
	}
	Formatters[reflect.TypeOf([]string{})] = func(val interface{}) string {
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, s := range val.([]string) {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(strconv.Quote(s))
		}
		buf.WriteByte(']')
		return fmt.Sprintf("%d:%s", StringSliceType, buf.String())
	}
	Formatters[reflect.TypeOf([]byte{})] = func(val interface{}) string {
		s := base64.RawStdEncoding.EncodeToString(val.([]byte))
		return fmt.Sprintf("%d:%s", ByteSliceType, s)
	}
	Formatters[reflect.TypeOf([]driver.Value{})] = func(val interface{}) string {
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, v := range val.([]driver.Value) {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(FormatWithType(v))
		}
		buf.WriteByte(']')
		return fmt.Sprintf("%d:%s", ValueSliceType, buf.String())
	}

	// Define parsers for standard sql package value types.
	Parsers = make(map[Type]func(val string) (interface{}, error))

	Parsers[NilType] = func(val string) (interface{}, error) {
		if val != "nil" {
			return nil, errors.New("expected nil")
		}
		return nil, nil
	}
	Parsers[StringType] = func(val string) (interface{}, error) {
		return strconv.Unquote(val)
	}
	Parsers[IntType] = func(val string) (interface{}, error) {
		return strconv.Atoi(val)
	}
	Parsers[Int64Type] = func(val string) (interface{}, error) {
		return strconv.ParseInt(val, 10, 64)
	}
	Parsers[Float64Type] = func(val string) (interface{}, error) {
		return strconv.ParseFloat(val, 64)
	}
	Parsers[BoolType] = func(val string) (interface{}, error) {
		if val == "false" {
			return false, nil
		} else if val == "true" {
			return true, nil
		}
		return nil, errors.New("expected true or false")
	}
	Parsers[ErrorType] = func(val string) (interface{}, error) {
		if val == "EOF" {
			// Return reference to singleton object so that callers can compare
			// by reference.
			return io.EOF, nil
		}
		s, err := strconv.Unquote(val)
		if err != nil {
			return nil, err
		}
		return errors.New(s), nil
	}
	Parsers[TimeType] = func(val string) (interface{}, error) {
		return time.Parse(time.RFC3339Nano, val)
	}
	Parsers[StringSliceType] = func(val string) (interface{}, error) {
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
	}
	Parsers[ByteSliceType] = func(val string) (interface{}, error) {
		return base64.RawStdEncoding.DecodeString(val)
	}
	Parsers[ValueSliceType] = func(val string) (interface{}, error) {
		slice, err := parseSlice(val)
		if err != nil {
			return nil, err
		}
		valueSlice := make([]driver.Value, len(slice))
		for i := range slice {
			valueSlice[i], err = ParseWithType(slice[i])
			if err != nil {
				return nil, err
			}
		}
		return valueSlice, nil
	}
}

// formatValueWithType converts the given value into a formatted string suitable
// for inclusion in a copyist recording file. The format is as follows:
//
//   <dataType>:<formattedValue>
//
// where dataType is the numeric value of the `copyist.Type` enumeration,
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
func FormatWithType(val interface{}) string {
	if val == nil {
		return fmt.Sprintf("%d:nil", NilType)
	}

	// Lookup formatter for the value.
	if formatter, ok := Formatters[reflect.TypeOf(val)]; ok {
		return formatter(val)
	}

	// No formatter found. Check for error interface special-case; otherwise
	// return error.
	switch t := val.(type) {
	case error:
		return fmt.Sprintf("%d:%s", ErrorType, strconv.Quote(t.Error()))
	default:
		panic(fmt.Errorf("unsupported type: %T", t))
	}
}

// ParseWithType parses a value from the copyist recording file, in the
// format produced by the `formatValueWithType` function:
//
//   <dataType>:<formattedValue>
//
// Only well-known "Type" data types are supported.
func ParseWithType(valWithTyp string) (interface{}, error) {
	index := strings.Index(valWithTyp, ":")
	if index == -1 {
		return nil, errors.New("expected colon")
	}
	num, err := strconv.Atoi(valWithTyp[:index])
	if err != nil {
		return nil, err
	}
	typ := Type(num)
	val := valWithTyp[index+1:]

	// Lookup parser for the value.
	if parser, ok := Parsers[typ]; ok {
		return parser(val)
	}

	// No parser for this type.
	return nil, fmt.Errorf("unsupported type: %v", typ)
}

// DeepCopyValue makes a deep copy of the given value. It is used to ensure that
// recorded values are immutable, and will never be updated by the application
// or driver. One case where this can happen is with driver.Rows.Next, where the
// storage for output values can be reused across calls to Next.
func DeepCopyValue(val interface{}) interface{} {
	switch t := val.(type) {
	case []string:
		return append([]string{}, t...)
	case []uint8:
		return append([]uint8{}, t...)
	case []driver.Value:
		newValues := make([]driver.Value, len(t))
		for i := range t {
			newValues[i] = DeepCopyValue(t[i])
		}
		return newValues
	default:
		// Most types don't need special handling.
		return t
	}
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
