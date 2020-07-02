# copyist
Mocking your SQL database in Go tests has never been easier. The copyist library
automatically records low-level SQL calls made during your tests. It then
generates test code that can play back those calls without connecting to the
real SQL database. Run your tests again. This time, they'll run much faster,
because now they do not require a database connection.

Best of all, your tests will run as if your test database was reset to a clean,
well-known state between every test case. Gone are the frustrating problems
where a test runs fine in isolation, but fails when run in concert with other
tests that modify the database.

copyist imposes no overhead on production code, and it requires almost no
changes to your application or testing code, as long as that code directly or
indirectly uses Go's `sql` package (e.g. Go ORM's and the widely used `sqlx`
package). This is because copyist runs at the driver level of Go's `sql`
package.

## What problems does copyist solve?
Imagine you have some application code that opens a connection to a Postgres
database and queries some customer data:
```
func QueryName(db *sql.DB) string {
	rows, _ := db.Query("SELECT name FROM customers WHERE id=$1", 100)
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		return name
	}
	return ""
}
```
The customary way to test this code would be to create a test database and
populate it with test customer data. However, what if application code modifies
rows in the database, like removing customers? If the above code runs on a
modified database, it may not return the expected customer. Therefore, it's
important to reset the state of the database between test cases so that tests
behave predictably. But connecting to a database is slow. Running queries is
slow. And resetting the state of an entire database between every test is
*really* slow.

Various mocking libraries are another alternative to using a test database.
These libraries intercept calls at some layer of the application or data access
stack, and return canned responses without needing to touch the database. The
problem with many of these libraries is that they require the developer to
manually construct the canned responses, which is time-consuming and fragile
when application changes occur.

## How does copyist solve these problems?
copyist includes a Go `sql` package driver that records the low-level SQL calls
made by application and test code. When a Go test using copyist is invoked with
the "-record" command-line flag, then the copyist driver will record all SQL
calls. When the test completes, copyist will generate playback Go code in a
companion test file. The Go test can then be run again without the "-record"
flag. This time the copyist driver will play back the recorded calls, without
needing to access the database. The Go test is none the wiser, and runs as if it
was using the database.

## How do I use copyist?
Below is the recommended test pattern for using copyist. The example shows how
to unit test the `QueryName` function shown above. 
```
func TestMain(m *testing.M) {
	flag.Parse()
	copyist.Register("postgres", resetDB)
	os.Exit(m.Run())
}

func TestQueryName(t *testing.T) {
	defer copyist.Open().Close()

	db, _ := sql.Open("copyist_postgres", "postgresql://root@localhost")
	defer db.Close()

	name := QueryName(db)
	if name != "Andy" {
		t.Error("failed test")
	}
}
```
In your `TestMain` function (or any other place that gets called before any of
tests), call te `copyist.Register` function. This function registers a new
driver with Go's `sql` package with the name `copyist_<driverName>`. In any
tests you'd like to record, add a `defer copyist.Open().Close()` statement.
This statement begins a new recording session, and then generates playback code
when `Close` is called at the end of the test.

copyist does need to know whether to run in "recording" mode or "playback" mode.
To make copyist run in "recording" mode, invoke the test with the `record` flag:
```
go test -run TestQueryName -record
``` 
This will generate a new test file in the same directory as the `TestQueryName`
file. For example, if that file is called `app_test.go`, then copyist will
generate an `app_copyist_test.go` file containing the recording for the
`TestQueryName` test. Now try running the test a couple more times (the first
time requires a recompile of the test, so will take longer):
```
go test -run TestQueryName
go test -run TestQueryName
```
It should now run significantly faster. 

## How do I reset the database between tests?
The above section glossed over an important detail. When registering a driver
for use with copyist, the second argument to `Register` is a callback function:
```
copyist.Register("postgres", resetDB)
``` 
If non-nil, this function will be called by copyist each time you call
`copyist.Open` (typically at the beginning of each test), as long as copyist is
running in "recording" mode. This reset function can do anything it likes, but
usually it will run a SQL script against the database in order to reset it to a
clean state, by dropping/creating tables, deleting data from table, and/or
inserting "fixture" data into tables that makes testing more convenient.

## Limitations
* Because of the way copyist works, it can only be used with non-parallel, single-
threaded tests and application code. This is because the driver code has no way
of knowing which connections, statements, and transactions are used by which
test and/or by which thread.

* copyist currently supports only the Postgres `pq` driver. If you'd like to
extend copyist to support other drivers, like MySql or SQLite, you're invited to
submit a pull request.
