module github.com/cockroachdb/copyist/pq/pqtest

go 1.16

require (
	github.com/cockroachdb/copyist v0.0.0-00010101000000-000000000000
	github.com/cockroachdb/copyist/drivertest v0.0.0-00010101000000-000000000000
	github.com/cockroachdb/copyist/pq v0.0.0-00010101000000-000000000000
	github.com/fortytw2/leaktest v1.3.0
	github.com/stretchr/testify v1.7.0
)

replace github.com/cockroachdb/copyist => ./../..

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist/pq => ./..

replace github.com/cockroachdb/copyist/drivertest => ./../../drivertest
