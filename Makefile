# Copyright 2021 The Cockroach Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
# implied. See the License for the specific language governing
# permissions and limitations under the License.

# test re-records all copyist tests from a clean state and then re-runs them
# using copyist playback. Note that the drivertest/pqtestold test package has to
# be run separately because it has its own go.mod file (see comment in that file
# for reasons why).
#
# NOTE: Run this before submitting a PR.
#
.PHONY: test
test:
	# Delete all copyist files and clear the test cache to get clean slate.
	@find . -type f -name '*.copyist' -delete
	# Re-record all tests.
	@COPYIST_RECORD=1 go test ./... -p=1 -count=1
	@cd drivertest/pqtestold && COPYIST_RECORD=1 go test ./... -p=1 -count=1
	# Run all tests using playback.
	@go test ./... -count=1
	@cd drivertest/pqtestold && go test ./... -count=1
