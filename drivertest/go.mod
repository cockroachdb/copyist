module github.com/cockroachdb/copyist/drivertest

go 1.16

// Use separate go.mod file so that importers of copyist do not need to deal
// with driver dependencies used only for testing copyist or the "replace"
// directives, in their own go.mod files.
require (
	github.com/cockroachdb/copyist v0.0.0-00010101000000-000000000000
	github.com/fortytw2/leaktest v1.3.0
	github.com/jackc/pgx/v4 v4.13.0
	github.com/jmoiron/sqlx v1.3.4
	github.com/lib/pq v1.10.2
	github.com/lib/pq/old v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.7.0
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./..

// Pull in ancient version of PQ, before support was added for QueryContext,
// ExecContext, and BeginTx, in order to test that copyist works even when
// drivers do not support those functions.
replace github.com/lib/pq/old => github.com/lib/pq v0.0.0-20170117202628-46f7bf5f8bd7
