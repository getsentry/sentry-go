package fakedriver

import (
	"context"
	"database/sql/driver"
	"io"
)

// ctxConn exposes the full context-aware interface set.
type ctxConn struct {
	drv *CtxDriver
}

func (c *ctxConn) Prepare(query string) (driver.Stmt, error) {
	return &ctxStmt{drv: c.drv, query: query}, nil
}

func (c *ctxConn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &ctxStmt{drv: c.drv, query: query}, nil
}

func (c *ctxConn) Close() error              { return nil }
func (c *ctxConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }
func (c *ctxConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}
func (c *ctxConn) Ping(_ context.Context) error { return nil }

func (c *ctxConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if c.drv.fail != nil {
		return nil, c.drv.fail
	}
	return fakeResult{}, nil
}

func (c *ctxConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.drv.fail != nil {
		return nil, c.drv.fail
	}
	return &fakeRows{}, nil
}

// legacyConn exposes only the pre-context interface set.
type legacyConn struct {
	drv *LegacyDriver
}

func (c *legacyConn) Prepare(query string) (driver.Stmt, error) {
	return &legacyStmt{drv: c.drv, query: query}, nil
}
func (c *legacyConn) Close() error              { return nil }
func (c *legacyConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

//nolint:staticcheck // intentional legacy driver.Execer implementation.
func (c *legacyConn) Exec(_ string, _ []driver.Value) (driver.Result, error) {
	if c.drv.fail != nil {
		return nil, c.drv.fail
	}
	return fakeResult{}, nil
}

//nolint:staticcheck // intentional legacy driver.Queryer implementation.
func (c *legacyConn) Query(_ string, _ []driver.Value) (driver.Rows, error) {
	if c.drv.fail != nil {
		return nil, c.drv.fail
	}
	return &fakeRows{}, nil
}

// minimalConn implements only the required driver.Conn methods. It is used to
// force database/sql down the Prepare + Stmt.Exec/Stmt.Query path, exercising
// the wrapper's deepest fallback branches.
type minimalConn struct {
	drv *MinimalDriver
}

func (c *minimalConn) Prepare(query string) (driver.Stmt, error) {
	return &minimalStmt{drv: c.drv, query: query}, nil
}
func (c *minimalConn) Close() error              { return nil }
func (c *minimalConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

// Compile-time interface guarantees.
var (
	_ driver.Conn               = (*ctxConn)(nil)
	_ driver.ConnPrepareContext = (*ctxConn)(nil)
	_ driver.ConnBeginTx        = (*ctxConn)(nil)
	_ driver.ExecerContext      = (*ctxConn)(nil)
	_ driver.QueryerContext     = (*ctxConn)(nil)
	_ driver.Pinger             = (*ctxConn)(nil)

	_ driver.Conn = (*legacyConn)(nil)
	//nolint:staticcheck
	_ driver.Execer = (*legacyConn)(nil)
	//nolint:staticcheck
	_ driver.Queryer = (*legacyConn)(nil)

	_ driver.Conn = (*minimalConn)(nil)
)

// fakeTx, fakeResult, and fakeRows are returned by the connection methods
// above. Keeping them here means a reader inspecting what Conn hands out
// doesn't need to jump files.

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 0, nil }

type fakeRows struct {
	exhausted bool
}

func (r *fakeRows) Columns() []string { return []string{"n"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.exhausted {
		return io.EOF
	}
	r.exhausted = true
	if len(dest) > 0 {
		dest[0] = int64(1)
	}
	return nil
}
