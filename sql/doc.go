// Package sentrysql provides Sentry instrumentation for database/sql drivers.
//
// It wraps an existing driver so that queries and executions become Sentry
// spans and are surfaced in the Queries module.
//
// Example:
//
//	import (
//	    _ "modernc.org/sqlite"
//	    "github.com/getsentry/sentry-go"
//	    sentrysql "github.com/getsentry/sentry-go/sql"
//	)
//
//	db, err := sentrysql.Open("sqlite", ":memory:",
//	    sentrysql.WithDatabaseSystem(sentrysql.SystemSQLite),
//	    sentrysql.WithDatabaseName("main"),
//	)
//
// For well-known drivers (postgres, pgx, mysql, sqlite, sqlite3, sqlserver,
// mssql, mariadb, oracle, godror, clickhouse, snowflake, ...), Open
// and Register can infer db.system from the registration name, so the option
// is optional:
//
//	db, err := sentrysql.Open("postgres", dsn) // db.system = "postgresql"
//
// OpenDB, WrapDriver, and WrapConnector cannot see a driver name and always
// require WithDatabaseSystem.
package sentrysql
