package sentrysql

import (
	"context"
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartSpan_NoParentReturnsNil(t *testing.T) {
	t.Parallel()
	span := startSpan(context.Background(), &config{system: SystemPostgreSQL}, opQuery, "SELECT 1")
	assert.Nil(t, span, "startSpan without parent must return nil")
}

func TestStartSpan_SpanData(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
		t.Cleanup(parent.Finish)

		cfg := &config{
			system:        SystemPostgreSQL,
			driverName:    "pgx",
			dbName:        "appdb",
			host:          "db.example.com",
			port:          5432,
			socketAddress: "10.0.0.1",
			socketPort:    5433,
		}
		span := startSpan(parent.Context(), cfg, opQuery, "SELECT 1")
		require.NotNil(t, span)

		assert.Equal(t, "SELECT 1", span.Description)
		assert.Equal(t, "SELECT 1", span.Data["db.query.text"])
		assert.Equal(t, "postgresql", span.Data["db.system.name"])
		assert.Equal(t, "pgx", span.Data["db.driver.name"])
		assert.Equal(t, "appdb", span.Data["db.namespace"])
		assert.Equal(t, "db.example.com", span.Data["server.address"])
		assert.Equal(t, 5432, span.Data["server.port"])
		assert.Equal(t, "10.0.0.1", span.Data["server.socket.address"])
		assert.Equal(t, 5433, span.Data["server.socket.port"])
		// db.user is absent — SendDefaultPII is off.
		assert.Nil(t, span.Data["db.user"])
	})
}

func TestStartSpan_UserEmittedOnlyWithPII(t *testing.T) {
	cfg := &config{
		system: SystemPostgreSQL,
		dbUser: "alice",
	}

	t.Run("PII off", func(t *testing.T) {
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			ctx := f.NewContext(context.Background())
			parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
			t.Cleanup(parent.Finish)

			span := startSpan(parent.Context(), cfg, opQuery, "SELECT 1")
			require.NotNil(t, span)
			assert.Nil(t, span.Data["db.user"], "db.user must not be set when SendDefaultPII is false")
		})
	})

	t.Run("PII on", func(t *testing.T) {
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			ctx := f.NewContext(context.Background())
			parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
			t.Cleanup(parent.Finish)

			span := startSpan(parent.Context(), cfg, opQuery, "SELECT 1")
			require.NotNil(t, span)
			assert.Equal(t, "alice", span.Data["db.user"], "db.user must be set when SendDefaultPII is true")
		}, sentrytest.WithClientOptions(sentry.ClientOptions{
			EnableTracing:    true,
			TracesSampleRate: 1.0,
			SendDefaultPII:   true,
		}))
	})
}

func TestFinishSpan_StatusMapping(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
		t.Cleanup(parent.Finish)

		okSpan := startSpan(parent.Context(), &config{system: SystemPostgreSQL}, opQuery, "SELECT 1")
		require.NotNil(t, okSpan, "startSpan must return a span when a parent is present")
		finishSpan(okSpan, nil)
		assert.Equal(t, sentry.SpanStatusOK, okSpan.Status)

		errSpan := startSpan(parent.Context(), &config{system: SystemPostgreSQL}, opExec, "INSERT INTO t VALUES (1)")
		finishSpan(errSpan, errors.New("boom"))
		assert.Equal(t, sentry.SpanStatusInternalError, errSpan.Status)
	})
}
