package sentrysql

import (
	"context"
	"database/sql/driver"
	"errors"
)

var (
	_ driver.Driver        = (*sentryDriver)(nil)
	_ driver.DriverContext = (*sentryDriver)(nil)
)

type sentryDriver struct {
	drv driver.Driver
	cfg *config
}

// newDriver returns a driver wrapper.
func newDriver(drv driver.Driver, cfg *config) driver.Driver {
	d := &sentryDriver{drv: drv, cfg: cfg}
	if _, ok := drv.(driver.DriverContext); ok {
		return d
	}
	// implement only driver.Driver
	return struct{ driver.Driver }{d}
}

// Open implements driver.Driver.
func (d *sentryDriver) Open(name string) (driver.Conn, error) {
	c, err := d.drv.Open(name)
	if err != nil {
		return nil, err
	}
	return newConn(c, d.cfg), nil
}

// OpenConnector implements driver.DriverContext.
func (d *sentryDriver) OpenConnector(name string) (driver.Connector, error) {
	dc, ok := d.drv.(driver.DriverContext)
	if !ok {
		return nil, errors.New("sentrysql: inner driver does not implement driver.DriverContext")
	}
	c, err := dc.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return newConnector(c, d.cfg), nil
}

// dsnConnector is a connector for drivers that do not implement
// driver.DriverContext.
type dsnConnector struct {
	dsn string
	drv driver.Driver
}

func (c *dsnConnector) Connect(_ context.Context) (driver.Conn, error) {
	return c.drv.Open(c.dsn)
}

func (c *dsnConnector) Driver() driver.Driver { return c.drv }
