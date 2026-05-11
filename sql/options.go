package sentrysql

import (
	"errors"
	"fmt"
	"strings"

	sqllexer "github.com/DataDog/go-sqllexer"
	"github.com/getsentry/sentry-go/sql/internal/dbsystem"
)

type DatabaseSystem = dbsystem.Name

const (
	SystemPostgreSQL = dbsystem.PostgreSQL
	SystemMySQL      = dbsystem.MySQL
	SystemMariaDB    = dbsystem.MariaDB
	SystemSQLite     = dbsystem.SQLite
	SystemMSSQL      = dbsystem.MSSQL
	SystemOracle     = dbsystem.Oracle
	SystemClickhouse = dbsystem.Clickhouse
	SystemSnowflake  = dbsystem.Snowflake
)

func systemFromDriverName(name string) (DatabaseSystem, bool) {
	return dbsystem.FromDriverName(name)
}

// Option configures sql wrappers.
type Option func(*config)

type config struct {
	system         DatabaseSystem
	dbName         string
	dbUser         string
	driverName     string
	host           string
	port           int
	socketAddress  string
	socketPort     int
	obfuscatorDBMS sqllexer.DBMSType
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

// WithDatabaseName sets the db.namespace span attribute (logical database
// name).
func WithDatabaseName(name string) Option {
	return func(c *config) { c.dbName = name }
}

// WithDatabaseUser sets the db.user span attribute.
func WithDatabaseUser(user string) Option {
	return func(c *config) { c.dbUser = user }
}

// WithDriverName sets the db.driver.name span attribute. Open and Register
// populate this automatically from the registered driver name; pass it
// explicitly when using OpenDB / WrapDriver / WrapConnector.
func WithDriverName(name string) Option {
	return func(c *config) { c.driverName = name }
}

// WithServerAddress sets the server.address (logical hostname) and
// server.port (logical port) span attributes. Pass port = 0 to omit it.
func WithServerAddress(host string, port int) Option {
	return func(c *config) {
		c.host = host
		c.port = port
	}
}

// WithServerSocketAddress sets the server.socket.address (physical IP or Unix
// socket path) and server.socket.port (physical port) span attributes. Pass
// port = 0 to omit it.
func WithServerSocketAddress(addr string, port int) Option {
	return func(c *config) {
		c.socketAddress = addr
		c.socketPort = port
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
	c.obfuscatorDBMS = obfuscatorDBMS(c.system)
	return c
}

func (c *config) obfuscateQuery(query string) string {
	if c == nil {
		return query
	}

	w := queryObfuscator{
		lexer:    newQueryLexer(query, c.obfuscatorDBMS),
		sqlite:   c.system == SystemSQLite,
		capacity: len(query),
	}
	return w.run()
}

func newQueryLexer(query string, dbms sqllexer.DBMSType) *sqllexer.Lexer {
	if dbms == "" {
		return sqllexer.New(query)
	}
	return sqllexer.New(query, sqllexer.WithDBMS(dbms))
}

type queryObfuscator struct {
	lexer            *sqllexer.Lexer
	sqlite           bool
	capacity         int
	out              strings.Builder
	prevPlaceholder  bool
	prevSQLiteQuoted bool
	pendingSQLiteDot bool
}

func (o *queryObfuscator) run() string {
	o.out.Grow(o.capacity)

	for {
		tok := o.lexer.Scan()
		switch tok.Type {
		case sqllexer.EOF:
			o.flushSQLiteDot()
			return strings.TrimSpace(o.out.String())
		case sqllexer.COMMENT, sqllexer.MULTILINE_COMMENT:
			continue
		case sqllexer.NUMBER, sqllexer.STRING, sqllexer.INCOMPLETE_STRING, sqllexer.DOLLAR_QUOTED_STRING, sqllexer.DOLLAR_QUOTED_FUNCTION:
			o.writePlaceholder(o.sqlite && tok.Type == sqllexer.STRING)
		case sqllexer.QUOTED_IDENT:
			if o.sqlite {
				o.writePlaceholder(true)
				continue
			}
			o.writeValue(tok.Value)
		case sqllexer.PUNCTUATION:
			if o.sqlite && tok.Value == "." && o.prevSQLiteQuoted {
				o.pendingSQLiteDot = true
				continue
			}
			o.writeValue(tok.Value)
		default:
			o.writeValue(tok.Value)
		}
	}
}

func (o *queryObfuscator) writePlaceholder(sqliteQuoted bool) {
	if o.pendingSQLiteDot {
		if sqliteQuoted && o.prevSQLiteQuoted {
			o.pendingSQLiteDot = false
			return
		}
		o.out.WriteByte('.')
		o.pendingSQLiteDot = false
	}
	if o.prevPlaceholder {
		return
	}
	o.out.WriteByte('?')
	o.prevPlaceholder = true
	o.prevSQLiteQuoted = sqliteQuoted
}

func (o *queryObfuscator) writeValue(value string) {
	o.flushSQLiteDot()
	o.out.WriteString(value)
	o.prevPlaceholder = false
	o.prevSQLiteQuoted = false
}

func (o *queryObfuscator) flushSQLiteDot() {
	if !o.pendingSQLiteDot {
		return
	}
	o.out.WriteByte('.')
	o.pendingSQLiteDot = false
}

func obfuscatorDBMS(system DatabaseSystem) sqllexer.DBMSType {
	switch system {
	case SystemPostgreSQL:
		return sqllexer.DBMSPostgres
	case SystemMySQL, SystemMariaDB, SystemSQLite:
		return sqllexer.DBMSMySQL
	case SystemMSSQL:
		return sqllexer.DBMSSQLServer
	case SystemOracle:
		return sqllexer.DBMSOracle
	case SystemSnowflake:
		return sqllexer.DBMSSnowflake
	default:
		return ""
	}
}
