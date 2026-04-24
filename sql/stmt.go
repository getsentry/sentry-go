package sentrysql

import (
	"context"
	"database/sql/driver"
)

// sentryStmt wraps a driver.Stmt.
type sentryStmt struct {
	stmt  driver.Stmt
	cfg   *config
	query string
}

func newStmt(s driver.Stmt, cfg *config, query string) driver.Stmt {
	return &sentryStmt{stmt: s, cfg: cfg, query: query}
}

// Close implements driver.Stmt.
func (s *sentryStmt) Close() error { return s.stmt.Close() }

// NumInput implements driver.Stmt.
func (s *sentryStmt) NumInput() int { return s.stmt.NumInput() }

// Exec implements driver.Stmt.
func (s *sentryStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.stmt.Exec(args) //nolint:staticcheck // required by driver.Stmt; ExecContext covers the modern path.
}

// Query implements driver.Stmt.
func (s *sentryStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.stmt.Query(args) //nolint:staticcheck // required by driver.Stmt; QueryContext covers the modern path.
}

// ExecContext implements driver.StmtExecContext with fallback to Exec.
func (s *sentryStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := s.stmt.(driver.StmtExecContext); ok {
		return ec.ExecContext(ctx, args)
	}
	values, err := namedValuesToValues(args)
	if err != nil {
		return nil, err
	}
	select {
	default:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.stmt.Exec(values) //nolint:staticcheck // legacy driver.Stmt.Exec fallback is intentional.
}

// QueryContext implements driver.StmtQueryContext with fallback to Query.
func (s *sentryStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := s.stmt.(driver.StmtQueryContext); ok {
		return qc.QueryContext(ctx, args)
	}
	values, err := namedValuesToValues(args)
	if err != nil {
		return nil, err
	}
	select {
	default:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.stmt.Query(values) //nolint:staticcheck // legacy driver.Stmt.Query fallback is intentional.
}

// CheckNamedValue implements driver.NamedValueChecker when the underlying
// statement supports it.
func (s *sentryStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if ch, ok := s.stmt.(driver.NamedValueChecker); ok {
		return ch.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// ColumnConverter implements driver.ColumnConverter when the underlying
// statement supports it.
func (s *sentryStmt) ColumnConverter(idx int) driver.ValueConverter {
	if cc, ok := s.stmt.(driver.ColumnConverter); ok { //nolint:staticcheck // ColumnConverter is deprecated but still honored by the stdlib.
		return cc.ColumnConverter(idx)
	}
	return driver.DefaultParameterConverter
}
