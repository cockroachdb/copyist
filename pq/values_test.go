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

package pq

import (
	"github.com/cockroachdb/copyist/values"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
	}{
		{"format pq.Error value", &pq.Error{
			Severity: pq.Efatal,
			Code: pq.ErrorCode("53200"),
			Message: "out of memory",
			Detail: "some detail",
			Hint: "some hint",
			Position: "123",
			InternalPosition: "456",
			InternalQuery: "some query",
			Where: "somewhere",
			Schema: "some schema",
			Table: "some table",
			Column: "some column",
			DataTypeName: "some datatype",
			Constraint: "some constraint",
			File: "some file",
			Line: "789",
			Routine: "some routine",
		}},
	}

	for _, cas := range cases {
		t.Run(cas.name, func(t *testing.T) {
			s := values.FormatWithType(cas.val)
			val, err := values.ParseWithType(s)
			require.NoError(t, err)
			require.Equal(t, cas.val, val)
		})
	}
}
