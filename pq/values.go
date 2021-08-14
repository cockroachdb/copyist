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
	"fmt"
	"reflect"
	"strconv"

	"github.com/cockroachdb/copyist/values"
	"github.com/jackc/pgproto3"
	"github.com/lib/pq"
)

func init() {
	// Register custom PQ types.
	values.Formatters[reflect.TypeOf(&pq.Error{})] = formatPqError
	values.Parsers[values.PqErrorType] = parsePqError
}

// formatPqError returns a lib/pq error as a string that is suitable for
// inclusion in a copyist recording file. It does this by using the pgproto3
// library to format the error using the Postgres wire protocol, and then encode
// it as a base64 string.
func formatPqError(val interface{}) string {
	pqErr := val.(*pq.Error)
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

	return fmt.Sprintf("%d:%s", values.PqErrorType, strconv.Quote(string(encoded)))
}

// parsePqError parses a string value that was formatted by formatPqError (minus
// the type prefix). This is expected to be Postgres wire protocol bytes for an
// error response, formatted as a quoted string.
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

// stringToInt32OrPanic converts the given string into an int32 value, or
// returns zero if it cannot (typically when string is empty).
func stringToInt32OrZero(val string) int32 {
	pos, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0
	}
	return int32(pos)
}
