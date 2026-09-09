package crosstest

import (
	"context"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
	sentryslog "github.com/getsentry/sentry-go/slog"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

type noopContextKey struct{}

func TestLogrusLogHookLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		logger := logrus.New()
		logger.AddHook(sentrylogrus.NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, f.Client))
		logger.WithContext(context.WithValue(otelCtx, noopContextKey{}, "noop")).Info("logrus linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "logrus linked log"))
	}, otelOpts()...)
}

func TestSlogLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		baseCtx := f.NewContext(context.Background())
		handler := sentryslog.Option{}.NewSentryHandler(baseCtx)
		logger := slog.New(handler)
		logger.InfoContext(f.NewContext(otelCtx), "slog linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "slog linked log"))
	}, otelOpts()...)
}

func TestZapLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		logger := zap.New(sentryzap.NewSentryCore(f.NewContext(otelCtx), sentryzap.Option{}))
		logger.Info("zap linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "zap linked log"))
	}, otelOpts()...)
}
