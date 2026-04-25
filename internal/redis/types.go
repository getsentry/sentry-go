package redis

// InstrumentationType selects which Sentry Insights module the hooks reports itself as.
type InstrumentationType int

const (
	// TypeCache reports spans to the Sentry Caches module.
	// This is the zero value and the default when Options is left empty.
	TypeCache InstrumentationType = iota

	// TypeDB reports db spans with scrubbed command descriptions.
	TypeDB
)

// DBSystem identifies the Redis-compatible database system for span attributes.
type DBSystem string

const (
	DBSystemValkey DBSystem = "valkey"
	DBSystemRedis  DBSystem = "redis"
)

// Address holds the parsed host and port of a Redis-compatible server.
type Address struct {
	Host string
	Port int
}

