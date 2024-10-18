package sentrysql

type SentrySqlTracerOption func(*sentrySqlConfig)

// WithDatabaseSystem specifies the current database system.
func WithDatabaseSystem(system DatabaseSystem) SentrySqlTracerOption {
	return func(config *sentrySqlConfig) {
		config.databaseSystem = system
	}
}

// WithDatabaseName specifies the name of the current database.
func WithDatabaseName(name string) SentrySqlTracerOption {
	return func(config *sentrySqlConfig) {
		config.databaseName = name
	}
}

// WithServerAddress specifies the address and port of the current database server.
func WithServerAddress(address string, port string) SentrySqlTracerOption {
	return func(config *sentrySqlConfig) {
		config.serverAddress = address
		config.serverPort = port
	}
}
