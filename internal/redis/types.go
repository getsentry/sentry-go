package redis

import "time"

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

const DefaultTimeout = 2 * time.Second

// Options configures the Sentry Redis like instrumentation hook.
type Options struct {
	// Type determines the Sentry Insights module.
	// TypeCache (default) reports cache.get / cache.put spans.
	// TypeDB reports db spans with scrubbed command descriptions.
	Type InstrumentationType

	// Timeout is the timeout for flushing Sentry events. Defaults to 2s.
	Timeout time.Duration
}
