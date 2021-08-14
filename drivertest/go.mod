module github.com/cockroachdb/copyist/drivertest

go 1.16

// Use separate go.mod file so that importers of copyist do not need to deal
// with testing driver dependencies and "replace" directives in their own go.mod
// files.
require (
	github.com/cockroachdb/copyist v0.0.0-00010101000000-000000000000
	github.com/fortytw2/leaktest v1.3.0
	github.com/jackc/pgx/v4 v4.10.1
	github.com/jmoiron/sqlx v1.3.4
	github.com/lib/pq v1.10.2
	github.com/stretchr/testify v1.7.0
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./..
