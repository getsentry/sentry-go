package sentrysql

import (
	"context"
	"database/sql/driver"

	"github.com/getsentry/sentry-go"
)

// sentryConn wraps the original driver.Conn.
// As per the driver's documentation:
//   - All Conn implementations should implement the following interfaces:
//     Pinger, SessionResetter, and Validator.
//   - If named parameters or context are supported, the driver's Conn should
//     implement: ExecerContext, QueryerContext, ConnPrepareContext,
//     and ConnBeginTx.
//
// On this specific Sentry wrapper, we are not going to implement the Validator
// interface because it does not support ErrSkip, since returning ErrSkip
// is only possible when it's explicitly stated on the driver documentation.
type sentryConn struct {
	originalConn driver.Conn
	ctx          context.Context
	config       *sentrySQLConfig
}

// Make sure that sentryConn implements the driver.Conn interface.
var _ driver.Conn = (*sentryConn)(nil)

func (s *sentryConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := s.originalConn.Prepare(query)
	if err != nil {
		return nil, err
	}

	return &sentryStmt{
		originalStmt: stmt,
		query:        query,
		ctx:          s.ctx,
		config:       s.config,
	}, nil
}

func (s *sentryConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	// should only be executed if the original driver implements ConnPrepareContext
	connPrepareContext, ok := s.originalConn.(driver.ConnPrepareContext)
	if !ok {
		// We can't return driver.ErrSkip here. We should fall back to Prepare without context.
		return s.Prepare(query)
	}

	stmt, err := connPrepareContext.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	return &sentryStmt{
		originalStmt: stmt,
		query:        query,
		ctx:          ctx,
		config:       s.config,
	}, nil
}

func (s *sentryConn) Close() error {
	return s.originalConn.Close()
}

func (s *sentryConn) Begin() (driver.Tx, error) {
	tx, err := s.originalConn.Begin() //nolint:staticcheck // We must support legacy clients
	if err != nil {
		return nil, err
	}

	return &sentryTx{originalTx: tx, ctx: s.ctx, config: s.config}, nil
}

func (s *sentryConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// should only be executed if the original driver implements ConnBeginTx
	connBeginTx, ok := s.originalConn.(driver.ConnBeginTx)
	if !ok {
		// We can't return driver.ErrSkip here. We should fall back to Begin without context.
		return s.Begin()
	}

	tx, err := connBeginTx.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &sentryTx{originalTx: tx, ctx: s.ctx, config: s.config}, nil
}

//nolint:dupl
func (s *sentryConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	// should only be executed if the original driver implements Queryer
	queryer, ok := s.originalConn.(driver.Queryer) //nolint:staticcheck // We must support legacy clients
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return queryer.Query(query, args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(query))
	s.config.SetData(span, query)
	defer span.Finish()

	rows, err := queryer.Query(query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

//nolint:dupl
func (s *sentryConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	// should only be executed if the original driver implements QueryerContext
	queryerContext, ok := s.originalConn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(ctx)
	if parentSpan == nil {
		return queryerContext.QueryContext(ctx, query, args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(query))
	s.config.SetData(span, query)
	defer span.Finish()

	rows, err := queryerContext.QueryContext(ctx, query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

//nolint:dupl
func (s *sentryConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	// should only be executed if the original driver implements Execer
	execer, ok := s.originalConn.(driver.Execer) //nolint:staticcheck // We must support legacy clients
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return execer.Exec(query, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(query))
	s.config.SetData(span, query)
	defer span.Finish()

	rows, err := execer.Exec(query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

//nolint:dupl
func (s *sentryConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// should only be executed if the original driver implements ExecerContext {
	execerContext, ok := s.originalConn.(driver.ExecerContext)
	if !ok {
		// ExecContext may return ErrSkip.
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(ctx)
	if parentSpan == nil {
		return execerContext.ExecContext(ctx, query, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(query))
	s.config.SetData(span, query)
	defer span.Finish()

	rows, err := execerContext.ExecContext(ctx, query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

func (s *sentryConn) Ping(ctx context.Context) error {
	pinger, ok := s.originalConn.(driver.Pinger)
	if !ok {
		// We may not return ErrSkip. We should return nil.
		return nil
	}

	return pinger.Ping(ctx)
}

func (s *sentryConn) ResetSession(ctx context.Context) error {
	sessionResetter, ok := s.originalConn.(driver.SessionResetter)
	if !ok {
		// We may not return ErrSkip. We should return nil.
		return nil
	}

	return sessionResetter.ResetSession(ctx)
}

func (s *sentryConn) CheckNamedValue(namedValue *driver.NamedValue) error {
	namedValueChecker, ok := s.originalConn.(driver.NamedValueChecker)
	if !ok {
		// We may return ErrSkip.
		return driver.ErrSkip
	}

	return namedValueChecker.CheckNamedValue(namedValue)
}
