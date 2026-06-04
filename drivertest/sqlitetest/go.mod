module github.com/cockroachdb/copyist/drivertest/sqlitetest

go 1.23

// Use a separate go.mod file (like drivertest/pqtestold) so that the large
// modernc.org/sqlite dependency, which also requires a much newer version of
// Go, is not added to the root copyist module.
require (
	github.com/cockroachdb/copyist v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.8.1
	modernc.org/sqlite v1.34.5
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgconn v1.14.3 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.3.3 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jmoiron/sqlx v1.3.4 // indirect
	github.com/lib/pq v1.10.2 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/crypto v0.20.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
)

// Reference copyist in the same repo.
replace github.com/cockroachdb/copyist => ./../..
