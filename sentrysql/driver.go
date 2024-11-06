package sentrysql

import (
	"context"
	"database/sql/driver"
	"io"
)

// sentrySQLDriver wraps the original driver.Driver.
// As per the driver's documentation:
// Drivers should implement driver.Connector and driver.DriverContext interfaces.
type sentrySQLDriver struct {
	originalDriver driver.Driver
	config         *sentrySQLConfig
}

// Make sure that sentrySQLDriver implements the driver.Driver interface.
var _ driver.Driver = (*sentrySQLDriver)(nil)
var _ driver.DriverContext = (*sentrySQLDriver)(nil)

func (s *sentrySQLDriver) OpenConnector(name string) (driver.Connector, error) {
	driverContext, ok := s.originalDriver.(driver.DriverContext)
	if !ok {
		return &sentrySQLConnector{
			originalConnector: dsnConnector{dsn: name, driver: s.originalDriver, config: s.config},
			config:            s.config,
		}, nil
	}

	connector, err := driverContext.OpenConnector(name)
	if err != nil {
		return nil, err
	}

	return &sentrySQLConnector{originalConnector: connector, config: s.config}, nil
}

func (s *sentrySQLDriver) Open(name string) (driver.Conn, error) {
	conn, err := s.originalDriver.Open(name)
	if err != nil {
		return nil, err
	}

	return &sentryConn{originalConn: conn, config: s.config}, nil
}

type sentrySQLConnector struct {
	originalConnector driver.Connector
	config            *sentrySQLConfig
}

// Make sure that sentrySQLConnector implements the driver.Connector interface.
var _ driver.Connector = (*sentrySQLConnector)(nil)
var _ io.Closer = (*sentrySQLConnector)(nil)

func (s *sentrySQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := s.originalConnector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	return &sentryConn{originalConn: conn, ctx: ctx, config: s.config}, nil
}

func (s *sentrySQLConnector) Driver() driver.Driver {
	return s.originalConnector.Driver()
}

func (s *sentrySQLConnector) Close() error {
	// driver.Connector should optionally implements io.Closer
	closer, ok := s.originalConnector.(io.Closer)
	if !ok {
		return nil
	}

	return closer.Close()
}

// dsnConnector is copied from
// https://cs.opensource.google/go/go/+/refs/tags/go1.23.2:src/database/sql/sql.go;l=795-806
type dsnConnector struct {
	dsn    string
	driver driver.Driver
	config *sentrySQLConfig
}

// Make sure dsnConnector implements driver.Connector.
var _ driver.Connector = (*dsnConnector)(nil)

func (t dsnConnector) Connect(_ context.Context) (driver.Conn, error) {
	return t.driver.Open(t.dsn)
}

func (t dsnConnector) Driver() driver.Driver {
	return t.driver
}
