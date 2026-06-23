package sentrysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartSpan_NoParentReturnsNil(t *testing.T) {
	t.Parallel()
	span := startQuerySpan(context.Background(), nil, &config{system: SystemPostgreSQL}, opQuery, "SELECT 1")
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
		span := startQuerySpan(parent.Context(), nil, cfg, opQuery, "SELECT 1")
		require.NotNil(t, span)

		assert.Equal(t, "SELECT ?", span.Description)
		assert.Equal(t, "SELECT ?", span.Data["db.query.text"])
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

	tests := []struct {
		name    string
		options sentry.ClientOptions
		want    any
	}{
		{
			name: "legacy default does not collect db.user",
			want: nil,
		},
		{
			name: "explicit spec default collects db.user",
			options: sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				DataCollection:   &sentry.DataCollection{},
			},
			want: "alice",
		},
		{
			name: "legacy SendDefaultPII on",
			options: sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				SendDefaultPII:   true,
			},
			want: "alice",
		},
		{
			name: "explicit DataCollection false overrides legacy true",
			options: sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				SendDefaultPII:   true,
				DataCollection: &sentry.DataCollection{
					UserInfo: sentry.Set(false),
				},
			},
			want: nil,
		},
		{
			name: "explicit DataCollection true enables db.user",
			options: sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				DataCollection: &sentry.DataCollection{
					UserInfo: sentry.Set(true),
				},
			},
			want: "alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				ctx := f.NewContext(context.Background())
				parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
				t.Cleanup(parent.Finish)

				span := startQuerySpan(parent.Context(), nil, cfg, opQuery, "SELECT 1")
				require.NotNil(t, span)
				assert.Equal(t, tt.want, span.Data["db.user"])
			}, sentrytest.WithClientOptions(tt.options))
		})
	}
}

func TestFinishSpan_StatusMapping(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
		t.Cleanup(parent.Finish)

		okSpan := startQuerySpan(parent.Context(), nil, &config{system: SystemPostgreSQL}, opQuery, "SELECT 1")
		require.NotNil(t, okSpan, "startSpan must return a span when a parent is present")
		finishSpan(okSpan, nil)
		assert.Equal(t, sentry.SpanStatusOK, okSpan.Status)

		errSpan := startQuerySpan(parent.Context(), nil, &config{system: SystemPostgreSQL}, opExec, "INSERT INTO t VALUES (1)")
		finishSpan(errSpan, errors.New("boom"))
		assert.Equal(t, sentry.SpanStatusInternalError, errSpan.Status)
	})
}

func TestStartQuerySpan_UsesTransactionParent(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
		t.Cleanup(parent.Finish)

		cfg := &config{system: SystemPostgreSQL}
		conn := &sentryConn{cfg: cfg}
		txSpan := startTxSpan(parent.Context(), cfg)
		require.NotNil(t, txSpan)
		conn.activeTx = newTx(nil, txSpan)

		querySpan := startQuerySpan(context.Background(), conn, cfg, opQuery, "SELECT 42")
		require.NotNil(t, querySpan)
		assert.Equal(t, txSpan.SpanID, querySpan.ParentSpanID)
		assert.Equal(t, "SELECT ?", querySpan.Description)
	})
}

type testConn struct{}

func (testConn) Prepare(string) (driver.Stmt, error) { return nil, nil }
func (testConn) Close() error                        { return nil }
func (testConn) Begin() (driver.Tx, error)           { return testTx{}, nil }

type testTx struct{}

func (testTx) Commit() error   { return nil }
func (testTx) Rollback() error { return nil }

func TestBeginClearsActiveTxSpan(t *testing.T) {
	conn := &sentryConn{conn: testConn{}, cfg: &config{system: SystemPostgreSQL}}
	conn.activeTx = newTx(nil, &sentry.Span{})

	tx, err := conn.Begin()
	require.NoError(t, err)
	assert.Nil(t, conn.txSpanOrNil())

	require.NoError(t, tx.Commit())
	assert.Nil(t, conn.txSpanOrNil())
}

func TestSentryTxFinishClearsConnSpan(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root", sentry.WithTransactionName("root"))
		t.Cleanup(parent.Finish)

		cfg := &config{system: SystemPostgreSQL}
		conn := &sentryConn{cfg: cfg}
		span := startTxSpan(parent.Context(), cfg)
		require.NotNil(t, span)

		tx := newTx(nil, span)
		conn.activeTx = tx
		tx.finish(nil, sentry.SpanStatusOK)

		assert.Nil(t, conn.txSpanOrNil())
		assert.Equal(t, sentry.SpanStatusOK, span.Status)
	})
}
