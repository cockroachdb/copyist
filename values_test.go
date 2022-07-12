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
	"errors"
	"github.com/jackc/pgconn"
	"io"
	"math"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
	}{
		{"format nil value", nil},
		{"format string value", "foo\n\t ][,"},
		{"format int value", int(-100)},
		{"format int64 value", math.MaxInt64},
		{"format float64 value", math.MaxFloat64},
		{"format Inf float64 value", math.Inf(+1)},
		{"format bool value", bool(true)},
		{"format error value", errors.New("some error\nmore stuff")},
		{"format EOF error value", io.EOF},
		{"format UTC time value", parseTime("2000-01-01T1:00:00Z")},
		{"format +0:00 time value", parseTime("2000-01-01T1:00:00.123456+00:00")},
		{"format timezone time value", parseTime("2000-01-01T1:00:00.123456789-07:00")},
		{"format string slice value", []string{"foo", "\n", "bar"}},
		{"format bytes value", []byte{0, 1, 2, 3, 4}},
		{"format driver.Value value", []driver.Value{0, []string{"foo", "bar"}, io.EOF}},
		{"format nested values", []driver.Value{[]driver.Value{0, nil}, "foo"}},
		{"format empty values", []driver.Value{"", []driver.Value{}, []string{}}},
		{"format slices with interesting tokens", []driver.Value{
			",][*// //* \"string\" range }{ `a string\n`",
			parseTime("2020-08-06T15:20:25.831116+00:00"),
			[]driver.Value{8, parseTime("2020-08-06T15:20:25.831116+00:00"), -8},
			"\n\t",
		}},
		{"format pq.Error value", &pq.Error{
			Severity:         pq.Efatal,
			Code:             pq.ErrorCode("53200"),
			Message:          "out of memory",
			Detail:           "some detail",
			Hint:             "some hint",
			Position:         "123",
			InternalPosition: "456",
			InternalQuery:    "some query",
			Where:            "somewhere",
			Schema:           "some schema",
			Table:            "some table",
			Column:           "some column",
			DataTypeName:     "some datatype",
			Constraint:       "some constraint",
			File:             "some file",
			Line:             "789",
			Routine:          "some routine",
		}},
		{"format pgconn.PgError value", &pgconn.PgError{
			Severity:         pq.Efatal,
			Code:             "53200",
			Message:          "out of memory",
			Detail:           "some detail",
			Hint:             "some hint",
			Position:         123,
			InternalPosition: 456,
			InternalQuery:    "some query",
			Where:            "somewhere",
			SchemaName:       "some schema",
			TableName:        "some table",
			ColumnName:       "some column",
			DataTypeName:     "some datatype",
			ConstraintName:   "some constraint",
			File:             "some file",
			Line:             789,
			Routine:          "some routine",
		}},
	}

	for _, cas := range cases {
		t.Run(cas.name, func(t *testing.T) {
			s := formatValueWithType(cas.val)
			val, err := parseValueWithType(s)
			require.NoError(t, err)
			require.Equal(t, cas.val, val)
		})
	}
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}
