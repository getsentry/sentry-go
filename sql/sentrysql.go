package sentrysql

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
)

// Open is a wrapper over sql.Open that provides Sentry instrumentation.
func Open(driverName, dataSourceName string, opts ...Option) (*sql.DB, error) {
	cfg := newConfig(opts)
	if cfg.system == "" {
		sys, ok := systemFromDriverName(driverName)
		if !ok {
			return nil, errSystemUnrecognized(driverName)
		}
		cfg.system = sys
	}
	drv, err := getDriver(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	wrapped := newDriver(drv, cfg)
	// Prefer driver.DriverContext for connection pooling semantics.
	if dc, ok := wrapped.(driver.DriverContext); ok {
		connector, err := dc.OpenConnector(dataSourceName)
		if err != nil {
			return nil, err
		}
		return sql.OpenDB(connector), nil
	}
	return sql.OpenDB(&dsnConnector{dsn: dataSourceName, drv: wrapped}), nil
}

// OpenDB wraps an existing driver.Connector and returns a *sql.DB.
func OpenDB(c driver.Connector, opts ...Option) (*sql.DB, error) {
	cfg := newConfig(opts)
	if cfg.system == "" {
		return nil, errSystemRequired
	}
	return sql.OpenDB(newConnector(c, cfg)), nil
}

// WrapDriver wraps a driver.Driver so connections it hands out are instrumented.
func WrapDriver(drv driver.Driver, opts ...Option) (driver.Driver, error) {
	cfg := newConfig(opts)
	if cfg.system == "" {
		return nil, errSystemRequired
	}
	return newDriver(drv, cfg), nil
}

// WrapConnector wraps a driver.Connector so connections it hands out are
// instrumented. WithDatabaseSystem is required.
func WrapConnector(c driver.Connector, opts ...Option) (driver.Connector, error) {
	cfg := newConfig(opts)
	if cfg.system == "" {
		return nil, errSystemRequired
	}
	return newConnector(c, cfg), nil
}

var (
	registerMu sync.Mutex
	registered = map[string]struct{}{}
)

// Register registers a wrapped version of drv under the name
// "sentrysql-<name>". It is safe to call once per process; subsequent calls
// with the same name return nil without re-registering.
func Register(name string, drv driver.Driver, opts ...Option) error {
	cfg := newConfig(opts)
	if cfg.system == "" {
		if sys, ok := systemFromDriverName(name); ok {
			cfg.system = sys
		} else {
			return errSystemUnrecognized(name)
		}
	}
	wrapped := newDriver(drv, cfg)
	registerMu.Lock()
	defer registerMu.Unlock()
	registeredName := "sentrysql-" + name
	if _, ok := registered[registeredName]; ok {
		return nil
	}
	sql.Register(registeredName, wrapped)
	registered[registeredName] = struct{}{}
	return nil
}

// getDriver opens and closes a driver to retrieve the driver implementation we need to wrap.
// We won't use the connection opened here, so we close it to avoid leaks.
func getDriver(name, dataSourceName string) (driver.Driver, error) {
	db, err := sql.Open(name, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("sentrysql: open driver %q: %w", name, err)
	}
	drv := db.Driver()
	if err = db.Close(); err != nil {
		return nil, fmt.Errorf("sentrysql: close driver %q: %w", name, err)
	}
	return drv, nil
}
