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

// Indirect calls to findTestFile, used by TestFindTestFile. It is important
// that these functions are in a different file than TestFindTestFile, and that
// the call depth is greater than 1.
func indirectFindTestFile1() string {
	return findTestFile()
}

func indirectFindTestFile() string {
	return indirectFindTestFile1()
}
