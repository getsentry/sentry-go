package dbsystem

// Name identifies the DBMS for the db.system span attribute. Use one of the
// provided constants, or a custom value that matches the Sentry Queries module
// expectations.
type Name string

// Known system names. This is not exhaustive; pass a custom string via
// Name("…") for databases not listed here.
const (
	PostgreSQL Name = "postgresql"
	MySQL      Name = "mysql"
	MariaDB    Name = "mariadb"
	SQLite     Name = "sqlite"
	MSSQL      Name = "mssql"
	Oracle     Name = "oracle"
	Clickhouse Name = "clickhouse"
	Snowflake  Name = "snowflake"
)

var driverNameToSystem = map[string]Name{
	"postgres":         PostgreSQL,
	"pgx":              PostgreSQL,
	"cloudsqlpostgres": PostgreSQL,
	"mysql":            MySQL,
	"mariadb":          MariaDB,
	"sqlite":           SQLite,
	"sqlite3":          SQLite,
	"sqlserver":        MSSQL,
	"mssql":            MSSQL,
	"oracle":           Oracle,
	"godror":           Oracle,
	"goora":            Oracle,
	"oci8":             Oracle,
	"clickhouse":       Clickhouse,
	"snowflake":        Snowflake,
}

func FromDriverName(name string) (Name, bool) {
	sys, ok := driverNameToSystem[name]
	return sys, ok
}
