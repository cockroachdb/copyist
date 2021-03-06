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

package commontest_test

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"

	"github.com/cockroachdb/copyist"
)

// TestNothing does not access the database at all.
func TestNothing(t *testing.T) {
	defer copyist.Open(t).Close()

	// Verify that no file was created in testdata.
	_, err := os.Stat("testdata/nothing_test.copyist")
	require.True(t, os.IsNotExist(err))
}
