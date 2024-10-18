package sentrysql

import (
	"context"
	"database/sql/driver"
)

type sentrySqlDriver struct {
	originalDriver driver.Driver
	config         *sentrySqlConfig
}

func (s *sentrySqlDriver) OpenConnector(name string) (driver.Connector, error) {
	driverContext, ok := s.originalDriver.(driver.DriverContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	connector, err := driverContext.OpenConnector(name)
	if err != nil {
		return nil, err
	}

	return &sentrySqlConnector{originalConnector: connector, config: s.config}, nil
}

func (s *sentrySqlDriver) Open(name string) (driver.Conn, error) {
	conn, err := s.originalDriver.Open(name)
	if err != nil {
		return nil, err
	}

	return &sentryConn{originalConn: conn, config: s.config}, nil
}

type sentrySqlConnector struct {
	originalConnector driver.Connector
	config            *sentrySqlConfig
}

func (s *sentrySqlConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := s.originalConnector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	return &sentryConn{originalConn: conn, ctx: ctx, config: s.config}, nil
}

func (s *sentrySqlConnector) Driver() driver.Driver {
	return s.originalConnector.Driver()
}
