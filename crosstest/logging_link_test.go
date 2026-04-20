package crosstest

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
	sentryslog "github.com/getsentry/sentry-go/slog"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

func TestLogrusLogHookLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		logger := logrus.New()
		logger.AddHook(sentrylogrus.NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, f.Client))
		logger.WithContext(context.WithValue(otelCtx, struct{}{}, "noop")).Info("logrus linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "logrus linked log"))
	}, otelOpts()...)
}

func TestLogrusEventHookLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		logger := logrus.New()
		logger.AddHook(sentrylogrus.NewEventHookFromClient([]logrus.Level{logrus.ErrorLevel}, f.Client))
		logger.WithContext(otelCtx).WithError(errors.New("logrus linked error event")).Error("logrus linked error event")

		f.Flush()
		requireLinked(t, f.Events(), linkedErrorEvent(traceID, spanID, "logrus linked error event"))
	}, otelOpts()...)
}

func TestSlogLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		baseCtx := context.Background()
		baseCtx = sentry.SetHubOnContext(baseCtx, f.Hub)
		handler := sentryslog.Option{}.NewSentryHandler(baseCtx)
		logger := slog.New(handler)
		logger.InfoContext(sentry.SetHubOnContext(otelCtx, f.Hub), "slog linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "slog linked log"))
	}, otelOpts()...)
}

func TestSlogEventLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
		handler := sentryslog.Option{
			EventLevel: []slog.Level{slog.LevelError},
			LogLevel:   []slog.Level{},
		}.NewSentryHandler(baseCtx)
		logger := slog.New(handler)
		logger.ErrorContext(sentry.SetHubOnContext(otelCtx, f.Hub), "slog linked error event")

		f.Flush()
		requireLinked(t, f.Events(), linkedErrorEvent(traceID, spanID, "slog linked error event"))
	}, otelOpts()...)
}

func TestZapLinksOTelTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		otelCtx, traceID, spanID := fixedOTelContext()

		logger := zap.New(sentryzap.NewSentryCore(sentry.SetHubOnContext(otelCtx, f.Hub), sentryzap.Option{}))
		logger.Info("zap linked log")

		f.Flush()
		requireLinked(t, f.Events(), linkedLogEvent(traceID, spanID, "zap linked log"))
	}, otelOpts()...)
}
