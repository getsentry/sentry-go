module github.com/getsentry/sentry-go/sentrysql

go 1.23.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.32.0
	github.com/glebarez/go-sqlite v1.21.1
	github.com/go-sql-driver/mysql v1.8.1
	github.com/google/go-cmp v0.5.9
	github.com/lib/pq v1.10.9
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	modernc.org/libc v1.22.3 // indirect
	modernc.org/mathutil v1.5.0 // indirect
	modernc.org/memory v1.5.0 // indirect
	modernc.org/sqlite v1.21.1 // indirect
)
