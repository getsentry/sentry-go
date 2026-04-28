package sentrysql

import (
	"errors"
	"fmt"
)

// DatabaseSystem identifies the DBMS for the db.system span attribute. Use one
// of the provided constants, or a custom value that matches the Sentry Queries
// module expectations.
type DatabaseSystem string

// Known DatabaseSystem values. This is not exhaustive; pass a custom string
// via DatabaseSystem("…") for databases not listed here.
const (
	SystemPostgreSQL DatabaseSystem = "postgresql"
	SystemMySQL      DatabaseSystem = "mysql"
	SystemMariaDB    DatabaseSystem = "mariadb"
	SystemSQLite     DatabaseSystem = "sqlite"
	SystemMSSQL      DatabaseSystem = "mssql"
	SystemOracle     DatabaseSystem = "oracle"
	SystemClickhouse DatabaseSystem = "clickhouse"
	SystemSnowflake  DatabaseSystem = "snowflake"
)

// driverNameToSystem is a best effort map of common Go SQL driver registration names.
var driverNameToSystem = map[string]DatabaseSystem{
	// PostgreSQL and flavors
	"postgres":         SystemPostgreSQL,
	"pgx":              SystemPostgreSQL,
	"cloudsqlpostgres": SystemPostgreSQL,
	// MySQL / MariaDB
	"mysql":   SystemMySQL,
	"mariadb": SystemMariaDB,
	// SQLite
	"sqlite":  SystemSQLite,
	"sqlite3": SystemSQLite,
	// MS SQL Server
	"sqlserver": SystemMSSQL,
	"mssql":     SystemMSSQL,
	// Oracle
	"oracle": SystemOracle,
	"godror": SystemOracle,
	"goora":  SystemOracle,
	"oci8":   SystemOracle,
	// Others
	"clickhouse": SystemClickhouse,
	"snowflake":  SystemSnowflake,
}

func systemFromDriverName(name string) (DatabaseSystem, bool) {
	sys, ok := driverNameToSystem[name]
	return sys, ok
}

// Option configures sql wrappers.
type Option func(*config)

type config struct {
	system DatabaseSystem
	dbName string
	host   string
	port   string
}

// WithDatabaseSystem sets the db.system span attribute. Prefer one of the
// System* constants; pass DatabaseSystem("...") for databases not enumerated.
//
// When Open is used and this option is omitted, sentrysql attempts to detect
// the system from the driver registration name. For OpenDB / WrapDriver /
// WrapConnector this option is required because the driver name is not
// available to the wrapper.
func WithDatabaseSystem(system DatabaseSystem) Option {
	return func(c *config) { c.system = system }
}

// WithDatabaseName sets the db.name span attribute.
func WithDatabaseName(name string) Option {
	return func(c *config) { c.dbName = name }
}

// WithServerAddress sets the server.address and server.port span attributes.
// Port may be empty.
func WithServerAddress(host, port string) Option {
	return func(c *config) {
		c.host = host
		c.port = port
	}
}

var errSystemRequired = errors.New("sentrysql: WithDatabaseSystem is required")

// errSystemUnrecognized is returned by Open when WithDatabaseSystem is omitted
// and the driver name is not in the autodetect table.
func errSystemUnrecognized(driverName string) error {
	return fmt.Errorf("sentrysql: unable to autodetect db.system from driver %q; pass sentrysql.WithDatabaseSystem(...) explicitly", driverName)
}

func newConfig(opts []Option) *config {
	c := &config{}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}
