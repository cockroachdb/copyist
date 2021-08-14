module github.com/cockroachdb/copyist/pq

go 1.16

require (
	github.com/cockroachdb/copyist v0.0.0-20210814043917-cedd93333e5b
	github.com/jackc/pgproto3 v1.1.0
	github.com/lib/pq v1.10.2
	github.com/stretchr/testify v1.7.0
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./..
