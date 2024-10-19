package sentrysql_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/sentrysql"
	sqlite "github.com/glebarez/go-sqlite"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/lib/pq"
)

func ExampleNewSentrySQL() {
	sql.Register("sentrysql-sqlite", sentrysql.NewSentrySQL(&sqlite.Driver{}, sentrysql.WithDatabaseName(":memory:"), sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("sqlite"))))

	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INT)")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("INSERT INTO test (id) VALUES (1)")
	if err != nil {
		panic(err)
	}

	rows, err := db.Query("SELECT * FROM test")
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		err = rows.Scan(&id)
		if err != nil {
			panic(err)
		}

		fmt.Println(id)
	}
}

func ExampleNewSentrySQLConnector() {
	pqConnector, err := pq.NewConnector("postgres://user:password@localhost:5432/db")
	if err != nil {
		panic(err)
	}

	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(pqConnector, sentrysql.WithDatabaseName("db"), sentrysql.WithDatabaseSystem(sentrysql.PostgreSQL), sentrysql.WithServerAddress("localhost", "5432")))
	defer db.Close()

	// Continue executing PostgreSQL queries
}

func TestIntegration(t *testing.T) {
	sql.Register("sentrysql-sqlite", sentrysql.NewSentrySQL(&sqlite.Driver{}, sentrysql.WithDatabaseName("memory"), sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("sqlite")), sentrysql.WithServerAddress("localhost", "5432")))

	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()

	setupQueries := []string{
		"CREATE TABLE exec_test (id INT, name TEXT)",
		"CREATE TABLE query_test (id INT, name TEXT, age INT, created_at TEXT)",
		"INSERT INTO query_test (id, name, age, created_at) VALUES (1, 'John', 30, '2023-01-01')",
		"INSERT INTO query_test (id, name, age, created_at) VALUES (2, 'Jane', 25, '2023-01-02')",
		"INSERT INTO query_test (id, name, age, created_at) VALUES (3, 'Bob', 35, '2023-01-03')",
	}

	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err = db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on sqlite: %v", err)
		}
	}

	t.Run("Query", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
		}{
			{
				Query:      "SELECT * FROM query_test WHERE id = ?",
				Parameters: []interface{}{1},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "SELECT",
					},
					Description: "SELECT * FROM query_test WHERE id = ?",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
		}

		spansCh := make(chan []*sentry.Span, len(tests))

		sentryClient, err := sentry.NewClient(sentry.ClientOptions{
			EnableTracing:    true,
			TracesSampleRate: 1.0,
			BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				spansCh <- event.Spans
				return event
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		for _, tt := range tests {
			hub := sentry.NewHub(sentryClient, sentry.NewScope())
			ctx := sentry.SetHubOnContext(context.Background(), hub)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			_, err := db.QueryContext(ctx, tt.Query, tt.Parameters...)
			if err != nil {
				t.Fatal(err)
			}

			span.Finish()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
		}

		optstrans := cmp.Options{
			cmpopts.IgnoreFields(
				sentry.Span{},
				"TraceID", "SpanID", "ParentSpanID", "StartTime", "EndTime",
				"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "collectProfile", "contexts",
			),
		}
		for i, tt := range tests {
			var foundMatch = false
			gotSpans := got[i]

			var diffs []string
			for _, gotSpan := range gotSpans {
				if diff := cmp.Diff(tt.WantSpan, gotSpan, optstrans); diff != "" {
					diffs = append(diffs, diff)
				} else {
					foundMatch = true
					break
				}
			}

			if !foundMatch {
				t.Errorf("Span mismatch (-want +got):\n%s", strings.Join(diffs, "\n"))
			}
		}
	})

}
