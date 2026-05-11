package sentrysql

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/getsentry/sentry-go"
)

const (
	opQuery = "db.sql.query"
	opExec  = "db.sql.exec"
)

// startSpan creates a child span for a SQL operation only
// when a parent span exists in the passed ctx.
func startSpan(ctx context.Context, cfg *config, op, query string) *sentry.Span {
	parent := sentry.SpanFromContext(ctx)
	if parent == nil {
		return nil
	}

	description := query
	if cfg != nil {
		description = cfg.obfuscateQuery(query)
	}

	span := parent.StartChild(op,
		sentry.WithDescription(description),
		sentry.WithSpanOrigin(SpanOrigin),
	)
	span.SetData("db.query.text", description)

	if cfg != nil {
		if cfg.system != "" {
			span.SetData("db.system.name", string(cfg.system))
		}
		if cfg.driverName != "" {
			span.SetData("db.driver.name", cfg.driverName)
		}
		if cfg.dbName != "" {
			span.SetData("db.namespace", cfg.dbName)
		}
		if cfg.dbUser != "" && sendDefaultPII(ctx) {
			span.SetData("db.user", cfg.dbUser)
		}
		if cfg.host != "" {
			span.SetData("server.address", cfg.host)
		}
		if cfg.port != 0 {
			span.SetData("server.port", cfg.port)
		}
		if cfg.socketAddress != "" {
			span.SetData("server.socket.address", cfg.socketAddress)
		}
		if cfg.socketPort != 0 {
			span.SetData("server.socket.port", cfg.socketPort)
		}
	}

	// TODO: add the remaining db span attributes once we have a proper query
	// analyzer for them:
	// - db.operation.name
	// - db.query.summary
	// - db.collection.name
	// - db.query.parameter.<key> behind the PII gate

	return span
}

func sendDefaultPII(ctx context.Context) bool {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	client := hub.Client()
	if client == nil {
		return false
	}
	return client.Options().SendDefaultPII
}

func finishSpan(span *sentry.Span, err error) {
	// driver.ErrSkip instructs the driver to fallback to the next available method. In these cases
	// we get two or more spans for the same transaction, so to avoid duplication we don't finish
	// these spans. Currently this works because the spanRecorder skips unfinished spans and doesn't add
	// them to the transactions. We might need a span.Discard in the future if this behavior changes.
	if span == nil || errors.Is(err, driver.ErrSkip) {
		return
	}
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}
	span.Finish()
}
