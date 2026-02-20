package sentrysql

type Option func(*sentrySQLConfig)

// WithDatabaseSystem specifies the current database system.
func WithDatabaseSystem(system DatabaseSystem) Option {
	return func(config *sentrySQLConfig) {
		config.databaseSystem = system
	}
}

// WithDatabaseName specifies the name of the current database.
func WithDatabaseName(name string) Option {
	return func(config *sentrySQLConfig) {
		config.databaseName = name
	}
}

// WithServerAddress specifies the address and port of the current database server.
func WithServerAddress(address string, port string) Option {
	return func(config *sentrySQLConfig) {
		config.serverAddress = address
		config.serverPort = port
	}
}

// AlwaysUseFallbackCommand makes the sentrysql to try another method if the
// SQL driver returns driver.ErrSkip when using ExecerContext or QueryerContext.
// This is useful for drivers that have partial support for context methods,
// but may result in duplicate spans for a single query.
//
// The default behavior is to not fallback and just mark the span with internal error
// status when driver.ErrSkip is returned.
func AlwaysUseFallbackCommand() Option {
	return func(config *sentrySQLConfig) {
		config.alwaysUseFallbackCommand = true
	}
}
