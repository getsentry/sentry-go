package sentrysql

import (
	"context"
	"database/sql/driver"

	"github.com/getsentry/sentry-go"
)

type sentryStmt struct {
	originalStmt driver.Stmt
	query        string
	ctx          context.Context
	config       *sentrySQLConfig
}

func (s *sentryStmt) Close() error {
	return s.originalStmt.Close()
}

func (s *sentryStmt) NumInput() int {
	return s.originalStmt.NumInput()
}

func (s *sentryStmt) Exec(args []driver.Value) (driver.Result, error) {
	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		//nolint:staticcheck We must support legacy clients
		return s.originalStmt.Exec(args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(s.query))
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

	//nolint:staticcheck We must support legacy clients
	result, err := s.originalStmt.Exec(args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.Finish()
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	span.Finish()

	return result, nil
}

func (s *sentryStmt) Query(args []driver.Value) (driver.Rows, error) {
	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		//nolint:staticcheck We must support legacy clients
		return s.originalStmt.Query(args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(s.query))
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

	//nolint:staticcheck We must support legacy clients
	rows, err := s.originalStmt.Query(args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.Finish()

		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	span.Finish()

	return rows, nil
}

func (s *sentryStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	// should only be executed if the original driver implements StmtExecContext
	stmtExecContext, ok := s.originalStmt.(driver.StmtExecContext)
	if !ok {
		// fallback to the so-called deprecated "Exec" method
		var values []driver.Value
		for _, nv := range args {
			values = append(values, nv.Value)
		}
		return s.Exec(values)
	}

	parentSpan := sentry.SpanFromContext(s.ctx)
	if parentSpan == nil {
		return stmtExecContext.ExecContext(ctx, args)
	}

	span := parentSpan.StartChild("db.sql.exec", sentry.WithDescription(s.query))
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

	result, err := stmtExecContext.ExecContext(ctx, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.Finish()
		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	span.Finish()

	return result, nil
}

func (s *sentryStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	// should only be executed if the original driver implements StmtQueryContext
	stmtQueryContext, ok := s.originalStmt.(driver.StmtQueryContext)
	if !ok {
		// fallback to the so-called deprecated "Query" method
		var values []driver.Value
		for _, nv := range args {
			values = append(values, nv.Value)
		}
		return s.Query(values)
	}

	parentSpan := sentry.SpanFromContext(ctx)
	if parentSpan == nil {
		return stmtQueryContext.QueryContext(ctx, args)
	}

	span := parentSpan.StartChild("db.sql.query", sentry.WithDescription(s.query))
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

	rows, err := stmtQueryContext.QueryContext(ctx, args)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.Finish()

		return nil, err
	}

	span.Status = sentry.SpanStatusOK
	span.Finish()

	return rows, nil
}

func (s *sentryStmt) CheckNamedValue(namedValue *driver.NamedValue) error {
	namedValueChecker, ok := s.originalStmt.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}

	return namedValueChecker.CheckNamedValue(namedValue)
}
