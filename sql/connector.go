package sentrysql

import (
	"context"
	"database/sql/driver"
	"io"
)

// sentryConnector wraps a driver.Connector so that returned connections are
// wrapped with sentryConn.
type sentryConnector struct {
	connector driver.Connector
	drv       driver.Driver
	cfg       *config
}

func newConnector(c driver.Connector, cfg *config) *sentryConnector {
	return &sentryConnector{
		connector: c,
		drv:       newDriver(c.Driver(), cfg),
		cfg:       cfg,
	}
}

// Connect implements driver.Connector.
func (c *sentryConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return newConn(conn, c.cfg), nil
}

// Driver implements driver.Connector.
func (c *sentryConnector) Driver() driver.Driver { return c.drv }

// Close checks if underlying connector implements io.Closer to Close
// the connection.
func (c *sentryConnector) Close() error {
	if cl, ok := c.connector.(io.Closer); ok {
		return cl.Close()
	}
	return nil
}
