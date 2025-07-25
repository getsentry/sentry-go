package sentryzap

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type tagField struct {
	Key   string
	Value string
}

func Tag(key string, value string) zap.Field {
	return zap.Field{Key: key, Type: zapcore.SkipType, Interface: tagField{key, value}}
}

type ctxField struct {
	Value context.Context
}

// Context adds a context to the logger.
// This can be used e.g. to pass trace information to sentry and allow linking events to their respective traces.
//
// See also https://docs.sentry.io/platforms/go/performance/instrumentation/opentelemetry/#linking-errors-to-transactions
func Context(ctx context.Context) zap.Field {
	return zap.Field{Key: "context", Type: zapcore.SkipType, Interface: ctxField{ctx}}
}
