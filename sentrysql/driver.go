package sentrysql

import (
	"context"
	"database/sql/driver"
	"io"
)

// sentrySQLDriver wraps the original driver.Driver.
// As per the driver's documentation:
// Drivers should implement driver.Connector and driver.DriverContext interfaces
type sentrySQLDriver struct {
	originalDriver driver.Driver
	config         *sentrySQLConfig
}

// Make sure that sentrySQLDriver implements the driver.Driver interface.
var _ driver.Driver = (*sentrySQLDriver)(nil)

func (s *sentrySQLDriver) OpenConnector(name string) (driver.Connector, error) {
	driverContext, ok := s.originalDriver.(driver.DriverContext)
	if !ok {
		return nil, driver.ErrSkip
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
