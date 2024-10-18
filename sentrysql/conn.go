package sentrysql

import (
	"context"
	"database/sql/driver"

	"github.com/getsentry/sentry-go"
)

type sentryConn struct {
	originalConn driver.Conn
	ctx          context.Context
	config       *sentrySqlConfig
}

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
		return nil, driver.ErrSkip
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
	tx, err := s.originalConn.Begin()
	if err != nil {
		return nil, err
	}

	return &sentryTx{originalTx: tx, ctx: s.ctx, config: s.config}, nil
}

func (s *sentryConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// should only be executed if the original driver implements ConnBeginTx
	connBeginTx, ok := s.originalConn.(driver.ConnBeginTx)
	if !ok {
		// fallback to the so-called deprecated "Begin" method
		return s.Begin()
	}

	tx, err := connBeginTx.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &sentryTx{originalTx: tx, ctx: s.ctx, config: s.config}, nil
}

func (s *sentryConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	// should only be executed if the original driver implements Queryer
	queryer, ok := s.originalConn.(driver.Queryer)
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return queryer.Query(query, args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(query))
	if s.config.databaseSystem != "" {
		span.SetData("db.system", s.config.databaseSystem)
	}
	if s.config.databaseName != "" {
		span.SetData("db.name", s.config.databaseName)
	}
	if s.config.serverAddress != "" {
		span.SetData("server.address", s.config.serverAddress)
	}
	if s.config.serverPort != "" {
		span.SetData("server.port", s.config.serverPort)
	}
	defer span.Finish()

	rows, err := queryer.Query(query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

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
	if s.config.databaseSystem != "" {
		span.SetData("db.system", s.config.databaseSystem)
	}
	if s.config.databaseName != "" {
		span.SetData("db.name", s.config.databaseName)
	}
	if s.config.serverAddress != "" {
		span.SetData("server.address", s.config.serverAddress)
	}
	if s.config.serverPort != "" {
		span.SetData("server.port", s.config.serverPort)
	}
	defer span.Finish()

	rows, err := queryerContext.QueryContext(ctx, query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

func (s *sentryConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	// should only be executed if the original driver implements Execer
	execer, ok := s.originalConn.(driver.Execer)
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return execer.Exec(query, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(query))
	if s.config.databaseSystem != "" {
		span.SetData("db.system", s.config.databaseSystem)
	}
	if s.config.databaseName != "" {
		span.SetData("db.name", s.config.databaseName)
	}
	if s.config.serverAddress != "" {
		span.SetData("server.address", s.config.serverAddress)
	}
	if s.config.serverPort != "" {
		span.SetData("server.port", s.config.serverPort)
	}
	defer span.Finish()

	rows, err := execer.Exec(query, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	return rows, nil
}

func (s *sentryConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// should only be executed if the original driver implements ExecerContext {
	execerContext, ok := s.originalConn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	parentSpan := sentry.SpanFromContext(ctx)
	if parentSpan == nil {
		return execerContext.ExecContext(ctx, query, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(query))
	if s.config.databaseSystem != "" {
		span.SetData("db.system", s.config.databaseSystem)
	}
	if s.config.databaseName != "" {
		span.SetData("db.name", s.config.databaseName)
	}
	if s.config.serverAddress != "" {
		span.SetData("server.address", s.config.serverAddress)
	}
	if s.config.serverPort != "" {
		span.SetData("server.port", s.config.serverPort)
	}
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
		return driver.ErrSkip
	}

	return pinger.Ping(ctx)
}

func (s *sentryConn) CheckNamedValue(namedValue *driver.NamedValue) error {
	namedValueChecker, ok := s.originalConn.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}

	return namedValueChecker.CheckNamedValue(namedValue)
}
