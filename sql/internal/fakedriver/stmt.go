package fakedriver

import (
	"context"
	"database/sql/driver"
)

// ctxStmt is returned by ctxConn.Prepare / PrepareContext.
type ctxStmt struct {
	drv   *CtxDriver
	query string
}

func (s *ctxStmt) Close() error  { return nil }
func (s *ctxStmt) NumInput() int { return -1 }

//nolint:staticcheck // required method on driver.Stmt.
func (s *ctxStmt) Exec(_ []driver.Value) (driver.Result, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return fakeResult{}, nil
}

//nolint:staticcheck // required method on driver.Stmt.
func (s *ctxStmt) Query(_ []driver.Value) (driver.Rows, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return &fakeRows{}, nil
}

func (s *ctxStmt) ExecContext(_ context.Context, _ []driver.NamedValue) (driver.Result, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return fakeResult{}, nil
}

func (s *ctxStmt) QueryContext(_ context.Context, _ []driver.NamedValue) (driver.Rows, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return &fakeRows{}, nil
}

// legacyStmt is returned by legacyConn.Prepare. It implements only the
// pre-context driver.Stmt interface.
type legacyStmt struct {
	drv   *LegacyDriver
	query string
}

func (s *legacyStmt) Close() error  { return nil }
func (s *legacyStmt) NumInput() int { return -1 }

//nolint:staticcheck // required method on driver.Stmt.
func (s *legacyStmt) Exec(_ []driver.Value) (driver.Result, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return fakeResult{}, nil
}

//nolint:staticcheck // required method on driver.Stmt.
func (s *legacyStmt) Query(_ []driver.Value) (driver.Rows, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return &fakeRows{}, nil
}

// minimalStmt is returned by minimalConn.Prepare. Like legacyStmt it implements
// only the required driver.Stmt interface, which is what forces the wrapper's
// sentryStmt.ExecContext / QueryContext fallbacks to route through the
// pre-context Stmt.Exec / Stmt.Query path.
type minimalStmt struct {
	drv   *MinimalDriver
	query string
}

func (s *minimalStmt) Close() error  { return nil }
func (s *minimalStmt) NumInput() int { return -1 }

//nolint:staticcheck // required method on driver.Stmt.
func (s *minimalStmt) Exec(_ []driver.Value) (driver.Result, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return fakeResult{}, nil
}

//nolint:staticcheck // required method on driver.Stmt.
func (s *minimalStmt) Query(_ []driver.Value) (driver.Rows, error) {
	if s.drv.fail != nil {
		return nil, s.drv.fail
	}
	return &fakeRows{}, nil
}
