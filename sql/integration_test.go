package sentrysql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentrysql "github.com/getsentry/sentry-go/sql"
	"github.com/getsentry/sentry-go/sql/internal/fakedriver"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type driverShape struct {
	name     string
	newDB    func(t *testing.T) *sql.DB
	wantData map[string]interface{}
}

func driverShapes(t *testing.T) []driverShape {
	t.Helper()

	ctxDrv := fakedriver.NewCtx()
	legacyDrv := fakedriver.NewLegacy()
	minDrv := fakedriver.NewMinimal()

	ctxName := fmt.Sprintf("fake-ctx-integration-%p", ctxDrv)
	legacyName := fmt.Sprintf("fake-legacy-integration-%p", legacyDrv)
	minName := fmt.Sprintf("fake-minimal-integration-%p", minDrv)
	fakedriver.Register(ctxName, ctxDrv)
	fakedriver.Register(legacyName, legacyDrv)
	fakedriver.Register(minName, minDrv)

	return []driverShape{
		{
			name: "CtxDriver",
			newDB: func(t *testing.T) *sql.DB {
				db, err := sentrysql.Open(ctxName, "",
					sentrysql.WithDatabaseSystem(sentrysql.SystemPostgreSQL),
					sentrysql.WithDatabaseName("appdb"),
					sentrysql.WithServerAddress("localhost", 5432),
				)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				return db
			},
			wantData: map[string]interface{}{
				"db.system.name": "postgresql",
				"db.driver.name": ctxName,
				"db.namespace":   "appdb",
				"server.address": "localhost",
				"server.port":    5432,
			},
		},
		{
			name: "LegacyDriver",
			newDB: func(t *testing.T) *sql.DB {
				db, err := sentrysql.Open(legacyName, "",
					sentrysql.WithDatabaseSystem(sentrysql.SystemMySQL),
					sentrysql.WithDatabaseName("appdb"),
					sentrysql.WithServerAddress("localhost", 3306),
				)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				return db
			},
			wantData: map[string]interface{}{
				"db.system.name": "mysql",
				"db.driver.name": legacyName,
				"db.namespace":   "appdb",
				"server.address": "localhost",
				"server.port":    3306,
			},
		},
		{
			name: "MinimalDriver",
			newDB: func(t *testing.T) *sql.DB {
				db, err := sentrysql.Open(minName, "",
					sentrysql.WithDatabaseSystem(sentrysql.SystemSQLite),
				)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				return db
			},
			wantData: map[string]interface{}{
				"db.system.name": "sqlite",
				"db.driver.name": minName,
			},
		},
	}
}

func tracingOpts() sentrytest.Option {
	return sentrytest.WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
}

func transactionEvents(f *sentrytest.Fixture) []*sentry.Event {
	var out []*sentry.Event
	for _, e := range f.Events() {
		if e.Type == "transaction" {
			out = append(out, e)
		}
	}
	return out
}

func TestIntegration_EmitsQueryAndExecSpans(t *testing.T) {
	t.Parallel()

	for _, shape := range driverShapes(t) {
		t.Run(shape.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				db := shape.newDB(t)

				ctx := f.NewContext(context.Background())
				parent := sentry.StartSpan(ctx, "root",
					sentry.WithTransactionName("root"))
				ctx = parent.Context()

				_, err := db.ExecContext(ctx, "INSERT INTO t VALUES (1)")
				require.NoError(t, err)

				rows, err := db.QueryContext(ctx, "SELECT * FROM t")
				require.NoError(t, err)
				_ = rows.Close()

				parent.Finish()

				f.Flush()

				txns := transactionEvents(f)
				require.Len(t, txns, 1)
				spans := txns[0].Spans
				require.GreaterOrEqual(t, len(spans), 2,
					"expected at least exec + query spans, got %d", len(spans))

				var gotExec, gotQuery *sentry.Span
				for _, s := range spans {
					switch s.Op {
					case "db.sql.exec":
						gotExec = s
					case "db.sql.query":
						gotQuery = s
					}
				}
				require.NotNil(t, gotExec, "missing db.sql.exec span")
				require.NotNil(t, gotQuery, "missing db.sql.query span")

				assert.Equal(t, parent.SpanID, gotExec.ParentSpanID,
					"exec span must be a direct child of the root transaction")
				assert.Equal(t, parent.SpanID, gotQuery.ParentSpanID,
					"query span must be a direct child of the root transaction")

				assert.Equal(t, sentrysql.SpanOrigin, gotExec.Origin)
				assert.Equal(t, sentrysql.SpanOrigin, gotQuery.Origin)
				assert.Equal(t, "INSERT INTO t VALUES (?)", gotExec.Description)
				assert.Equal(t, "SELECT * FROM t", gotQuery.Description)
				assert.Equal(t, sentry.SpanStatusOK, gotExec.Status)
				assert.Equal(t, sentry.SpanStatusOK, gotQuery.Status)

				assert.NotEmpty(t, gotExec.Data["db.system.name"])
				assert.Equal(t, "INSERT INTO t VALUES (?)", gotExec.Data["db.query.text"])
				assert.Equal(t, "SELECT * FROM t", gotQuery.Data["db.query.text"])
			}, tracingOpts())
		})
	}
}

func TestIntegration_TransactionSpansParentQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		finish     func(*sql.Tx) error
		wantStatus sentry.SpanStatus
	}{
		{name: "Commit", finish: func(tx *sql.Tx) error { return tx.Commit() }, wantStatus: sentry.SpanStatusOK},
		{name: "Rollback", finish: func(tx *sql.Tx) error { return tx.Rollback() }, wantStatus: sentry.SpanStatusAborted},
	}

	for _, shape := range driverShapes(t) {
		shape := shape
		for _, tc := range tests {
			tc := tc
			t.Run(shape.name+"/"+tc.name, func(t *testing.T) {
				sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
					db := shape.newDB(t)

					ctx := f.NewContext(context.Background())
					parent := sentry.StartSpan(ctx, "root",
						sentry.WithTransactionName("root"))
					ctx = parent.Context()

					tx, err := db.BeginTx(ctx, nil)
					require.NoError(t, err)

					_, err = tx.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
					require.NoError(t, err)

					rows, err := tx.QueryContext(context.Background(), "SELECT * FROM t WHERE id = 42")
					require.NoError(t, err)
					_ = rows.Close()

					require.NoError(t, tc.finish(tx))
					parent.Finish()
					f.Flush()

					txns := transactionEvents(f)
					require.Len(t, txns, 1)

					var txnSpan, execSpan, querySpan *sentry.Span
					for _, span := range txns[0].Spans {
						switch span.Op {
						case "db.sql.transaction":
							txnSpan = span
						case "db.sql.exec":
							execSpan = span
						case "db.sql.query":
							querySpan = span
						}
					}

					require.NotNil(t, txnSpan, "missing db.sql.transaction span")
					require.NotNil(t, execSpan, "missing db.sql.exec span")
					require.NotNil(t, querySpan, "missing db.sql.query span")

					assert.Equal(t, parent.SpanID, txnSpan.ParentSpanID)
					assert.Equal(t, txnSpan.SpanID, execSpan.ParentSpanID)
					assert.Equal(t, txnSpan.SpanID, querySpan.ParentSpanID)
					assert.Equal(t, tc.wantStatus, txnSpan.Status)
					assert.Equal(t, sentrysql.SpanOrigin, txnSpan.Origin)
					assert.Equal(t, "transaction", txnSpan.Description)
					assert.Equal(t, "INSERT INTO t VALUES (?)", execSpan.Description)
					assert.Equal(t, "SELECT * FROM t WHERE id = ?", querySpan.Description)

					wantExecData := maps.Clone(shape.wantData)
					wantExecData["db.query.text"] = "INSERT INTO t VALUES (?)"
					wantQueryData := maps.Clone(shape.wantData)
					wantQueryData["db.query.text"] = "SELECT * FROM t WHERE id = ?"

					if diff := cmp.Diff(shape.wantData, txnSpan.Data); diff != "" {
						t.Fatalf("transaction span data mismatch (-want +got):\n%s", diff)
					}
					if diff := cmp.Diff(wantExecData, execSpan.Data); diff != "" {
						t.Fatalf("exec span data mismatch (-want +got):\n%s", diff)
					}
					if diff := cmp.Diff(wantQueryData, querySpan.Data); diff != "" {
						t.Fatalf("query span data mismatch (-want +got):\n%s", diff)
					}
				}, tracingOpts())
			})
		}
	}
}

func TestIntegration_TransactionExecObfuscatesPII(t *testing.T) {
	t.Parallel()

	const query = "INSERT INTO users (email, password) VALUES ('alice@example.com', 'super-secret')"

	for _, shape := range driverShapes(t) {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				db := shape.newDB(t)

				ctx := f.NewContext(context.Background())
				parent := sentry.StartSpan(ctx, "root",
					sentry.WithTransactionName("root"))

				tx, err := db.BeginTx(parent.Context(), nil)
				require.NoError(t, err)

				_, err = tx.ExecContext(context.Background(), query)
				require.NoError(t, err)

				require.NoError(t, tx.Commit())
				parent.Finish()
				f.Flush()

				txns := transactionEvents(f)
				require.Len(t, txns, 1)

				for _, span := range txns[0].Spans {
					assert.NotContains(t, span.Description, "alice@example.com")
					assert.NotContains(t, span.Description, "super-secret")

					queryText, _ := span.Data["db.query.text"].(string)
					assert.NotContains(t, queryText, "alice@example.com")
					assert.NotContains(t, queryText, "super-secret")
				}
			}, tracingOpts())
		})
	}
}

func TestIntegration_ErrSkipDoesNotDuplicateSpans(t *testing.T) {
	t.Parallel()

	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		drv := fakedriver.NewSkip()
		name := fmt.Sprintf("fake-skip-integration-%p", drv)
		fakedriver.Register(name, drv)

		db, err := sentrysql.Open(name, "",
			sentrysql.WithDatabaseSystem(sentrysql.SystemPostgreSQL),
			sentrysql.WithDatabaseName("appdb"),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		ctx := f.NewContext(context.Background())
		parent := sentry.StartSpan(ctx, "root",
			sentry.WithTransactionName("root"))
		ctx = parent.Context()

		_, err = db.ExecContext(ctx, "INSERT INTO t VALUES (1)")
		require.NoError(t, err)

		rows, err := db.QueryContext(ctx, "SELECT * FROM t")
		require.NoError(t, err)
		_ = rows.Close()

		parent.Finish()
		f.Flush()

		txns := transactionEvents(f)
		require.Len(t, txns, 1)
		require.Len(t, txns[0].Spans, 2, "ErrSkip fallback must record exactly one span per operation")

		var execCount, queryCount int
		for _, span := range txns[0].Spans {
			assert.Equal(t, parent.SpanID, span.ParentSpanID)
			assert.Equal(t, sentry.SpanStatusOK, span.Status)

			switch span.Op {
			case "db.sql.exec":
				execCount++
			case "db.sql.query":
				queryCount++
			}
		}

		assert.Equal(t, 1, execCount, "exec fallback must not create a duplicate span")
		assert.Equal(t, 1, queryCount, "query fallback must not create a duplicate span")
	}, tracingOpts())
}

func TestIntegration_ErrorStatusPropagates(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name    string
		newDrv  func() driver.Driver
		setFail func(d driver.Driver, err error)
		system  sentrysql.DatabaseSystem
	}{
		{
			name:   "CtxDriver",
			newDrv: func() driver.Driver { return fakedriver.NewCtx() },
			setFail: func(d driver.Driver, err error) {
				d.(*fakedriver.CtxDriver).SetFailure(err)
			},
			system: sentrysql.SystemPostgreSQL,
		},
		{
			name:   "LegacyDriver",
			newDrv: func() driver.Driver { return fakedriver.NewLegacy() },
			setFail: func(d driver.Driver, err error) {
				d.(*fakedriver.LegacyDriver).SetFailure(err)
			},
			system: sentrysql.SystemMySQL,
		},
		{
			name:   "MinimalDriver",
			newDrv: func() driver.Driver { return fakedriver.NewMinimal() },
			setFail: func(d driver.Driver, err error) {
				d.(*fakedriver.MinimalDriver).SetFailure(err)
			},
			system: sentrysql.SystemSQLite,
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				drv := shape.newDrv()
				shape.setFail(drv, fakedriver.ErrDriver)
				name := fmt.Sprintf("fake-err-%s-%p", shape.name, drv)
				fakedriver.Register(name, drv)

				db, err := sentrysql.Open(name, "",
					sentrysql.WithDatabaseSystem(shape.system),
				)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })

				ctx := f.NewContext(context.Background())
				parent := sentry.StartSpan(ctx, "root",
					sentry.WithTransactionName("root"))
				ctx = parent.Context()

				_, err = db.ExecContext(ctx, "INSERT INTO t VALUES (1)")
				require.True(t, errors.Is(err, fakedriver.ErrDriver),
					"driver error must propagate: %v", err)

				parent.Finish()
				f.Flush()

				txns := transactionEvents(f)
				require.Len(t, txns, 1)
				require.NotEmpty(t, txns[0].Spans)

				var execSpan *sentry.Span
				for _, s := range txns[0].Spans {
					if s.Op == "db.sql.exec" {
						execSpan = s
						break
					}
				}
				require.NotNil(t, execSpan, "missing db.sql.exec span")
				assert.Equal(t, sentry.SpanStatusInternalError, execSpan.Status)
			}, tracingOpts())
		})
	}
}

func TestIntegration_NoParentSpanEmitsNothing(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		fakedriver.Register("fake-ctx-nops", fakedriver.NewCtx())
		db, err := sentrysql.Open("fake-ctx-nops", "",
			sentrysql.WithDatabaseSystem(sentrysql.SystemPostgreSQL),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		ctx := f.NewContext(context.Background())
		_, err = db.ExecContext(ctx, "SELECT 1")
		require.NoError(t, err)

		f.Flush()
		assert.Empty(t, transactionEvents(f),
			"no spans must be captured when ctx has no parent span")
	}, tracingOpts())
}
