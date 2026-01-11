package sentrysql

import (
	"database/sql/driver"
	"strings"

	"github.com/DataDog/go-sqllexer"
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

func (d DatabaseSystem) toDbmsType() sqllexer.DBMSType {
	switch d {
	case PostgreSQL:
		return sqllexer.DBMSPostgres
	case MySQL:
		return sqllexer.DBMSMySQL
	case MSSQL:
		return sqllexer.DBMSSQLServer
	case SQLite:
		return sqllexer.DBMSPostgres // Close enough
	default:
		// XXX(aldy505): Should be fine if the DBMS type is empty string.
		return ""
	}
}

type sentrySQLConfig struct {
	databaseSystem           DatabaseSystem
	databaseName             string
	serverAddress            string
	serverPort               string
	alwaysUseFallbackCommand bool
}

var obfuscator = sqllexer.NewObfuscator(sqllexer.WithReplaceBindParameter(false), sqllexer.WithReplacePositionalParameter(false), sqllexer.WithReplaceBoolean(false), sqllexer.WithReplaceDigits(false), sqllexer.WithReplaceNull(false))
var normalizer = sqllexer.NewNormalizer(sqllexer.WithCollectCommands(true), sqllexer.WithCollectTables(true), sqllexer.WithCollectProcedures(true), sqllexer.WithRemoveSpaceBetweenParentheses(true), sqllexer.WithUppercaseKeywords(true), sqllexer.WithKeepIdentifierQuotation(true))

func (s *sentrySQLConfig) SetData(span *sentry.Span, query string) {
	if span == nil {
		return
	}

	queryClone := strings.Clone(query)

	if s.databaseSystem != "" {
		span.SetData("db.system.name", s.databaseSystem)
	}
	if s.databaseName != "" {
		span.SetData("db.namespace", s.databaseName)
	}
	if s.serverAddress != "" {
		span.SetData("server.address", s.serverAddress)
	}
	if s.serverPort != "" {
		span.SetData("server.port", s.serverPort)
	}

	if queryClone != "" {
		dbmsType := s.databaseSystem.toDbmsType()
		normalizedSQL, statementMetadata, err := sqllexer.ObfuscateAndNormalize(queryClone, obfuscator, normalizer, sqllexer.WithDBMS(dbmsType))
		if err != nil {
			// XXX(aldy505): What to do?????
			databaseOperation := parseDatabaseOperation(queryClone)
			if databaseOperation != "" {
				span.SetData("db.operation.name", databaseOperation)
			}
			return
		}

		span.Description = normalizedSQL
		span.SetData("db.query.text", normalizedSQL)
		if statementMetadata != nil {
			if len(statementMetadata.Tables) > 0 {
				span.SetData("db.collection.name", statementMetadata.Tables[0])
			}
			if len(statementMetadata.Commands) > 0 {
				span.SetData("db.operation.name", statementMetadata.Commands[0])
			} else {
				// Fallback to parsing the operation from the query if procedure is not found.
				databaseOperation := parseDatabaseOperation(queryClone)
				if databaseOperation != "" {
					span.SetData("db.operation.name", databaseOperation)
				}
			}
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
