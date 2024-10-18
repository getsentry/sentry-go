package sentrysql

import "database/sql/driver"

// DatabaseSystem points to the list of accepted OpenTelemetry database system.
// The ones defined here are not exhaustive, but are the ones that are supported by Sentry.
// Although you can override the value by creating your own, it will still be sent to Sentry,
// but it most likely will not appear on the Queries Insights page.
type DatabaseSystem string

const (
	PostgreSQL DatabaseSystem = "postgresql"
	MySQL      DatabaseSystem = "mysql"
	SQLite     DatabaseSystem = "sqlite"
	Oracle     DatabaseSystem = "oracle"
	MSSQL      DatabaseSystem = "mssql"
)

type sentrySqlConfig struct {
	databaseSystem DatabaseSystem
	databaseName   string
	serverAddress  string
	serverPort     string
}

// NewSentrySql is a wrapper for driver.Driver that provides tracing for SQL queries.
// The span will only be created if the parent span is available.
func NewSentrySql(driver driver.Driver, options ...SentrySqlTracerOption) driver.Driver {
	var config sentrySqlConfig
	for _, option := range options {
		option(&config)
	}

	return &sentrySqlDriver{originalDriver: driver, config: &config}
}

// NewSentrySqlConnector is a wrapper for driver.Connector that provides tracing for SQL queries.
// The span will only be created if the parent span is available.
func NewSentrySqlConnector(connector driver.Connector, options ...SentrySqlTracerOption) driver.Connector {
	var config sentrySqlConfig
	for _, option := range options {
		option(&config)
	}

	return &sentrySqlConnector{originalConnector: connector, config: &config}
}
