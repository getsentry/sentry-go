package sentrysql

import (
	"context"

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

	span := parent.StartChild(op,
		sentry.WithDescription(query),
		sentry.WithSpanOrigin(SpanOrigin),
	)

	span.SetData("db.query.text", query)

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

	// TODO: on the next PR we add the query parser, we then need to set:
	// - db.operation.name
	// - db.query.summary
	// - db.collection.name
	// - db.query.parameter.<key> PII gate

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
	if span == nil {
		return
	}
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}
	span.Finish()
}
