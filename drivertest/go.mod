module github.com/cockroachdb/copyist/drivertest

go 1.15

// Use separate go.mod file so that importers of copyist do not need to deal
// with testing driver dependencies and "replace" directives in their own go.mod
// files.
require (
	github.com/cockroachdb/copyist v0.0.0-00000000000000-000000000000
	github.com/fortytw2/leaktest v1.3.0
	github.com/jackc/pgx/v4 v4.10.1
	github.com/jmoiron/sqlx v1.3.1
	github.com/lib/pq v1.7.0
	github.com/lib/pq/old v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.7.0
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./..

// Pull in ancient version of PQ, before support was added for QueryContext,
// ExecContext, and BeginTx, in order to test that copyist works even when
// drivers do not support those functions.
replace github.com/lib/pq/old => github.com/lib/pq v0.0.0-20170117202628-46f7bf5f8bd7
