package sentrysql

type SentrySQLOption func(*sentrySQLConfig)

// WithDatabaseSystem specifies the current database system.
func WithDatabaseSystem(system DatabaseSystem) SentrySQLOption {
	return func(config *sentrySQLConfig) {
		config.databaseSystem = system
	}
}

// WithDatabaseName specifies the name of the current database.
func WithDatabaseName(name string) SentrySQLOption {
	return func(config *sentrySQLConfig) {
		config.databaseName = name
	}
}

// WithServerAddress specifies the address and port of the current database server.
func WithServerAddress(address string, port string) SentrySQLOption {
	return func(config *sentrySQLConfig) {
		config.serverAddress = address
		config.serverPort = port
	}
}
