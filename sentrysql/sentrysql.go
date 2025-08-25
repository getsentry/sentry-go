package sentrysql

import (
	"database/sql/driver"

	"github.com/getsentry/sentry-go"
)

// DatabaseSystem points to the list of accepted OpenTelemetry database system.
// The ones defined here are not exhaustive, but are the ones that are supported by Sentry.
// Although you can override the value by creating your own, it will still be sent to Sentry,
// but it most likely will not appear on the Queries Insights page.
type DatabaseSystem string

const (
	// PostgreSQL specifies the PostgreSQL database system.
	PostgreSQL DatabaseSystem = "postgresql"
	// MySQL specifies the MySQL database system.
	MySQL DatabaseSystem = "mysql"
	// SQLite specifies the SQLite database system.
	SQLite DatabaseSystem = "sqlite"
	// Oracle specifies the Oracle database system.
	Oracle DatabaseSystem = "oracle"
	// MSSQL specifies the Microsoft SQL Server database system.
	MSSQL DatabaseSystem = "mssql"
)

type sentrySQLConfig struct {
	databaseSystem DatabaseSystem
	databaseName   string
	serverAddress  string
	serverPort     string
}

func (s *sentrySQLConfig) SetData(span *sentry.Span, query string) {
	if span == nil {
		return
	}

	if s.databaseSystem != "" {
		span.SetData("db.system", s.databaseSystem)
	}
	if s.databaseName != "" {
		span.SetData("db.name", s.databaseName)
	}
	if s.serverAddress != "" {
		span.SetData("server.address", s.serverAddress)
	}
	if s.serverPort != "" {
		span.SetData("server.port", s.serverPort)
	}

	if query != "" {
		databaseOperation := parseDatabaseOperation(query)
		if databaseOperation != "" {
			span.SetData("db.operation", databaseOperation)
		}
	}
}

// NewSentrySQL is a wrapper for driver.Driver that provides tracing for SQL queries.
// The span will only be created if the parent span is available.
func NewSentrySQL(driver driver.Driver, options ...Option) driver.Driver {
	var config sentrySQLConfig
	for _, option := range options {
		option(&config)
	}

	return &sentrySQLDriver{originalDriver: driver, config: &config}
}

// NewSentrySQLConnector is a wrapper for driver.Connector that provides tracing for SQL queries.
// The span will only be created if the parent span is available.
func NewSentrySQLConnector(connector driver.Connector, options ...Option) driver.Connector {
	var config sentrySQLConfig
	for _, option := range options {
		option(&config)
	}

	return &sentrySQLConnector{originalConnector: connector, config: &config}
}
