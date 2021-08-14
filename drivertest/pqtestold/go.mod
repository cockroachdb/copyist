module github.com/cockroachdb/copyist/drivertest/pqtestold

go 1.16

// Use separate go.mod file so that ancient version of PQ can be tested, before
// support was added for QueryContext, ExecContext, and BeginTx. This enables
// testing that copyist works even when drivers do not support those functions.
require (
	github.com/cockroachdb/copyist v0.0.0-00010101000000-000000000000
	github.com/lib/pq v1.10.2
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./../..

// Override the latest version of lib/pq with an ancient version.
replace github.com/lib/pq => github.com/lib/pq v0.0.0-20170117202628-46f7bf5f8bd7
