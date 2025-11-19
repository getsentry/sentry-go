package sentrysql

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/getsentry/sentry-go"
)

type sentryStmt struct {
	originalStmt driver.Stmt
	query        string
	ctx          context.Context
	config       *sentrySQLConfig
}

// Make sure sentryStmt implements driver.Stmt interface.
var _ driver.Stmt = (*sentryStmt)(nil)
var _ driver.StmtExecContext = (*sentryStmt)(nil)
var _ driver.StmtQueryContext = (*sentryStmt)(nil)
var _ driver.NamedValueChecker = (*sentryStmt)(nil)

func (s *sentryStmt) Close() error {
	return s.originalStmt.Close()
}

func (s *sentryStmt) NumInput() int {
	return s.originalStmt.NumInput()
}

//nolint:dupl
func (s *sentryStmt) Exec(args []driver.Value) (driver.Result, error) {
	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return s.originalStmt.Exec(args) //nolint:staticcheck // We must support legacy clients
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(s.query))
	s.config.SetData(span, s.query)
	defer span.Finish()

	result, err := s.originalStmt.Exec(args) //nolint:staticcheck // We must support legacy clients
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK

	return result, nil
}

//nolint:dupl
func (s *sentryStmt) Query(args []driver.Value) (driver.Rows, error) {
	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return s.originalStmt.Query(args) //nolint:staticcheck // We must support legacy clients
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(s.query))
	s.config.SetData(span, s.query)
	defer span.Finish()

	rows, err := s.originalStmt.Query(args) //nolint:staticcheck // We must support legacy clients
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

func (s *sentryStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	// should only be executed if the original driver implements StmtExecContext
	stmtExecContext, ok := s.originalStmt.(driver.StmtExecContext)
	if !ok {
		// We may not return driver.ErrSkip. We should fallback to Exec without context.
		values, err := namedValueToValue(args)
		if err != nil {
			return nil, err
		}

		s.ctx = ctx
		return s.Exec(values)
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return stmtExecContext.ExecContext(ctx, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(s.query))
	s.config.SetData(span, s.query)
	defer span.Finish()

	result, err := stmtExecContext.ExecContext(span.Context(), args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK

	return result, nil
}

func (s *sentryStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	// should only be executed if the original driver implements StmtQueryContext
	stmtQueryContext, ok := s.originalStmt.(driver.StmtQueryContext)
	if !ok {
		// We may not return driver.ErrSkip. We should fallback to Exec without context.
		values, err := namedValueToValue(args)
		if err != nil {
			return nil, err
		}

		s.ctx = ctx
		return s.Query(values)
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return stmtQueryContext.QueryContext(ctx, args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(s.query))
	s.config.SetData(span, s.query)
	defer span.Finish()

	rows, err := stmtQueryContext.QueryContext(span.Context(), args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

func (s *sentryStmt) CheckNamedValue(namedValue *driver.NamedValue) error {
	// It is allowed to return driver.ErrSkip if the original driver does not
	// implement driver.NamedValueChecker.
	namedValueChecker, ok := s.originalStmt.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}

	return namedValueChecker.CheckNamedValue(namedValue)
}

// namedValueToValue is an exact copy of
// https://cs.opensource.google/go/go/+/refs/tags/go1.23.2:src/database/sql/ctxutil.go;l=137-146
func namedValueToValue(named []driver.NamedValue) ([]driver.Value, error) {
	dargs := make([]driver.Value, len(named))
	for n, param := range named {
		if len(param.Name) > 0 {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		dargs[n] = param.Value
	}
	return dargs, nil
}
