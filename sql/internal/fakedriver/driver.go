// Package fakedriver is a minimal in-memory database/sql driver used by the
// sentrysql tests. It provides three distinct driver shapes:
//
//   - CtxDriver: implements driver.Driver and driver.DriverContext; connections
//     satisfy ExecerContext, QueryerContext, ConnPrepareContext, ConnBeginTx,
//     and Pinger.
//   - LegacyDriver: implements only driver.Driver (no DriverContext); connections
//     implement the pre-context Execer, Queryer, Conn.Begin, and Conn.Prepare.
//   - MinimalDriver: implements only driver.Driver; connections implement only
//     the required driver.Conn methods (Prepare, Close, Begin). No Execer or
//     Queryer, which forces database/sql to fall back to Prepare + Stmt.Exec/
//     Stmt.Query, exercising the deepest wrapper fallback paths.
//
// Use NewCtx / NewLegacy / NewMinimal to construct each shape and Register to
// expose it by name via database/sql.
package fakedriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"sync/atomic"
)

// CtxDriver is a modern driver that implements driver.DriverContext in addition
// to driver.Driver. Connections it returns satisfy the full context-aware
// interface set.
type CtxDriver struct {
	fail error
}

// NewCtx returns a new CtxDriver.
func NewCtx() *CtxDriver { return &CtxDriver{} }

// SetFailure makes subsequent Exec/Query calls return err. Pass nil to clear.
func (d *CtxDriver) SetFailure(err error) { d.fail = err }

// Open implements driver.Driver.
func (d *CtxDriver) Open(_ string) (driver.Conn, error) {
	return &ctxConn{drv: d}, nil
}

// OpenConnector implements driver.DriverContext.
func (d *CtxDriver) OpenConnector(_ string) (driver.Connector, error) {
	return &ctxConnector{drv: d}, nil
}

type ctxConnector struct{ drv *CtxDriver }

func (c *ctxConnector) Connect(_ context.Context) (driver.Conn, error) { return c.drv.Open("") }
func (c *ctxConnector) Driver() driver.Driver                          { return c.drv }

// LegacyDriver implements only the pre-context driver.Driver interface.
type LegacyDriver struct {
	fail error
}

// NewLegacy returns a new LegacyDriver.
func NewLegacy() *LegacyDriver { return &LegacyDriver{} }

// SetFailure makes subsequent Exec/Query calls return err. Pass nil to clear.
func (d *LegacyDriver) SetFailure(err error) { d.fail = err }

// Open implements driver.Driver.
func (d *LegacyDriver) Open(_ string) (driver.Conn, error) {
	return &legacyConn{drv: d}, nil
}

// MinimalDriver implements only driver.Driver. Connections it returns
// implement only the required driver.Conn methods — no Execer, Queryer,
// ConnBeginTx, ConnPrepareContext, or Pinger. Used to exercise the wrapper's
// deepest fallback path where ExecContext returns driver.ErrSkip and
// database/sql falls back to Prepare + Stmt.Exec/Stmt.Query.
type MinimalDriver struct {
	fail error
}

// NewMinimal returns a new MinimalDriver.
func NewMinimal() *MinimalDriver { return &MinimalDriver{} }

// SetFailure makes subsequent Stmt.Exec/Stmt.Query calls return err. Pass nil
// to clear.
func (d *MinimalDriver) SetFailure(err error) { d.fail = err }

// Open implements driver.Driver.
func (d *MinimalDriver) Open(_ string) (driver.Conn, error) {
	return &minimalConn{drv: d}, nil
}

// CtxConnector is an exported connector wrapper backed by a CtxDriver. Unlike
// the internal ctxConnector returned by CtxDriver.OpenConnector, this one
// implements io.Closer with an observable close counter so tests can verify
// that the sentrysql wrapper propagates DB.Close through to inner connectors.
type CtxConnector struct {
	drv   *CtxDriver
	count atomic.Int32
}

// NewCtxConnector constructs a CtxConnector.
func NewCtxConnector(drv *CtxDriver) *CtxConnector { return &CtxConnector{drv: drv} }

// Connect implements driver.Connector.
func (c *CtxConnector) Connect(_ context.Context) (driver.Conn, error) { return c.drv.Open("") }

// Driver implements driver.Connector.
func (c *CtxConnector) Driver() driver.Driver { return c.drv }

// Close implements io.Closer. The counter it increments is observable via
// CloseCount for verification in tests.
func (c *CtxConnector) Close() error { c.count.Add(1); return nil }

// CloseCount reports how many times Close was invoked on this connector.
func (c *CtxConnector) CloseCount() int { return int(c.count.Load()) }

// LegacyConnector wraps a LegacyDriver as a driver.Connector so tests can
// exercise the sentrysql wrapper's behavior when a connector's Driver() does
// not implement driver.DriverContext. It does not implement io.Closer.
type LegacyConnector struct {
	drv *LegacyDriver
}

// NewLegacyConnector constructs a LegacyConnector.
func NewLegacyConnector(drv *LegacyDriver) *LegacyConnector { return &LegacyConnector{drv: drv} }

// Connect implements driver.Connector.
func (c *LegacyConnector) Connect(_ context.Context) (driver.Conn, error) { return c.drv.Open("") }

// Driver implements driver.Connector.
func (c *LegacyConnector) Driver() driver.Driver { return c.drv }

// Compile-time interface guarantees.
var (
	_ driver.Driver        = (*CtxDriver)(nil)
	_ driver.DriverContext = (*CtxDriver)(nil)

	_ driver.Driver = (*LegacyDriver)(nil)

	_ driver.Driver = (*MinimalDriver)(nil)

	_ driver.Connector = (*CtxConnector)(nil)
	_ driver.Connector = (*LegacyConnector)(nil)
)

var (
	registerMu sync.Mutex
	registered = map[string]struct{}{}
)

// Register registers d under name with database/sql. Safe to call multiple
// times with the same name.
func Register(name string, d driver.Driver) {
	registerMu.Lock()
	defer registerMu.Unlock()
	if _, ok := registered[name]; ok {
		return
	}
	sql.Register(name, d)
	registered[name] = struct{}{}
}

// ErrDriver is a reusable error for failure-injection tests.
var ErrDriver = errors.New("fakedriver: error")
