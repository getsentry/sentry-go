package sentrysql_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/sentrysql"
	sqlite "github.com/glebarez/go-sqlite"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var optstrans = cmp.Options{
	cmpopts.IgnoreFields(
		sentry.Span{},
		"TraceID", "SpanID", "ParentSpanID", "StartTime", "EndTime",
		"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "collectProfile", "contexts",
	),
}

func TestMain(m *testing.M) {
	sql.Register("sentrysql-sqlite", sentrysql.NewSentrySQL(&sqlite.Driver{}, sentrysql.WithDatabaseName("memory"), sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("sqlite")), sentrysql.WithServerAddress("localhost", "5432")))
	// sentrysql-legacy is used by `sentrysql_legacy_test.go`
	sql.Register("sentrysql-legacy", sentrysql.NewSentrySQL(ldriver, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("legacydb")), sentrysql.WithDatabaseName("fake")))

	os.Exit(m.Run())
}

//nolint:dupl
func TestNewSentrySQL_Integration(t *testing.T) {
	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
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

	t.Run("QueryContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
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
			{
				Query:     "SELECT FROM query_test",
				WantError: true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "SELECT",
					},
					Description: "SELECT FROM query_test",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			rows, err := db.QueryContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			if rows != nil {
				_ = rows.Close()
			}

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

	t.Run("ExecContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Parameters: []interface{}{1, "John"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "UPDATE exec_test SET name = ? WHERE id = ?",
				Parameters: []interface{}{"Bob", 1},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "UPDATE",
					},
					Description: "UPDATE exec_test SET name = ? WHERE id = ?",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "DELETE FROM exec_test WHERE name = ?",
				Parameters: []interface{}{"Nolan"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "DELETE",
					},
					Description: "DELETE FROM exec_test WHERE name = ?",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{
					1, "John", "Doe", 1,
				},
				WantError: true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
				},
			},
			{
				Query:     "CREATE TABLE temporary_test (id INT, name TEXT)",
				WantError: false,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
					},
					Description: "CREATE TABLE temporary_test (id INT, name TEXT)",
					Op:          "db.sql.exec",
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			_, err := db.ExecContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

	t.Run("Ping", func(t *testing.T) {
		// Just checking if this works and doesn't panic
		err := db.Ping()
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("PingContext", func(t *testing.T) {
		// Just checking if this works and doesn't panic
		err := db.PingContext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Driver", func(t *testing.T) {
		// Just checking if this works and doesn't panic
		driver := db.Driver()
		if driver == nil {
			t.Fatal("driver is nil")
		}
	})
}

//nolint:dupl
func TestNewSentrySQL_Conn(t *testing.T) {
	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
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

	t.Run("QueryContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
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
			{
				Query:      "SELECT FROM query_test",
				Parameters: []interface{}{1},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "SELECT",
					},
					Description: "SELECT FROM query_test",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			conn, err := db.Conn(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			rows, err := conn.QueryContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				_ = conn.Close()
				cancel()
				t.Fatal(err)
			}

			if rows != nil {
				_ = rows.Close()
			}

			_ = conn.Close()

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

	t.Run("ExecContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Parameters: []interface{}{2, "Peter"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			conn, err := db.Conn(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			_, err = conn.ExecContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				_ = conn.Close()
				cancel()
				t.Fatal(err)
			}

			_ = conn.Close()

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

//nolint:dupl,gocyclo
func TestNewSentrySQL_BeginTx(t *testing.T) {
	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
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

	t.Run("Singles", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Parameters: []interface{}{2, "Peter"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			conn, err := db.Conn(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			_, err = tx.ExecContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				_ = conn.Close()
				cancel()
				t.Fatal(err)
			}

			err = tx.Commit()
			if err != nil && !tt.WantError {
				_ = conn.Close()
				cancel()
				t.Fatal(err)
			}

			_ = tx.Rollback()

			_ = conn.Close()

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

	t.Run("Multiple Queries", func(t *testing.T) {
		spansCh := make(chan []*sentry.Span, 2)

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

		hub := sentry.NewHub(sentryClient, sentry.NewScope())
		ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
		defer cancel()
		span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
		ctx = span.Context()

		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		var name string
		err = tx.QueryRowContext(ctx, "SELECT name FROM query_test WHERE id = ?", 1).Scan(&name)
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_, err = tx.ExecContext(ctx, "INSERT INTO exec_test (id, name) VALUES (?, ?)", 5, "Catherine")
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		err = tx.Commit()
		if err != nil {
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_ = conn.Close()

		span.Finish()

		cancel()

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got []*sentry.Span
		for e := range spansCh {
			got = append(got, e...)
		}

		want := []*sentry.Span{
			{
				Data: map[string]interface{}{
					"db.system":      sentrysql.DatabaseSystem("sqlite"),
					"db.name":        "memory",
					"server.address": "localhost",
					"server.port":    "5432",
					"db.operation":   "SELECT",
				},
				Description: "SELECT name FROM query_test WHERE id = ?",
				Op:          "db.sql.query",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
			{
				Data: map[string]interface{}{
					"db.system":      sentrysql.DatabaseSystem("sqlite"),
					"db.name":        "memory",
					"server.address": "localhost",
					"server.port":    "5432",
					"db.operation":   "INSERT",
				},
				Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Op:          "db.sql.exec",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
		}

		if diff := cmp.Diff(want, got, optstrans); diff != "" {
			t.Errorf("Span mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		spansCh := make(chan []*sentry.Span, 2)

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

		hub := sentry.NewHub(sentryClient, sentry.NewScope())
		ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
		defer cancel()
		span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
		ctx = span.Context()

		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		var name string
		err = tx.QueryRowContext(ctx, "SELECT name FROM query_test WHERE id = ?", 1).Scan(&name)
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_, err = tx.ExecContext(ctx, "INSERT INTO exec_test (id, name) VALUES (?, ?)", 5, "Catherine")
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		err = tx.Rollback()
		if err != nil {
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_ = conn.Close()

		span.Finish()

		cancel()

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got []*sentry.Span
		for e := range spansCh {
			got = append(got, e...)
		}

		want := []*sentry.Span{
			{
				Data: map[string]interface{}{
					"db.system":      sentrysql.DatabaseSystem("sqlite"),
					"db.name":        "memory",
					"server.address": "localhost",
					"server.port":    "5432",
					"db.operation":   "SELECT",
				},
				Description: "SELECT name FROM query_test WHERE id = ?",
				Op:          "db.sql.query",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
			{
				Data: map[string]interface{}{
					"db.system":      sentrysql.DatabaseSystem("sqlite"),
					"db.name":        "memory",
					"server.address": "localhost",
					"server.port":    "5432",
					"db.operation":   "INSERT",
				},
				Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Op:          "db.sql.exec",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
		}

		if diff := cmp.Diff(want, got, optstrans); diff != "" {
			t.Errorf("Span mismatch (-want +got):\n%s", diff)
		}
	})
}

//nolint:dupl
func TestNewSentrySQL_PrepareContext(t *testing.T) {
	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
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

	t.Run("Exec", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Parameters: []interface{}{3, "Sarah"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			stmt, err := db.PrepareContext(ctx, tt.Query)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			_, err = stmt.Exec(tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

	t.Run("Query", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "SELECT * FROM query_test WHERE id = ?",
				Parameters: []interface{}{2},
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
			{
				Query:      "SELECT * FROM query_test WHERE id =",
				Parameters: []interface{}{1},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.DatabaseSystem("sqlite"),
						"db.name":        "memory",
						"server.address": "localhost",
						"server.port":    "5432",
						"db.operation":   "SELECT",
					},
					Description: "SELECT * FROM query_test WHERE id =",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			stmt, err := db.PrepareContext(ctx, tt.Query)
			if err != nil {
				cancel()
				t.Fatal(err)
			}

			rows, err := stmt.Query(tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			if rows != nil {
				_ = rows.Close()
			}

			span.Finish()
			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
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

//nolint:dupl
func TestNewSentrySQL_NoParentSpan(t *testing.T) {
	db, err := sql.Open("sentrysql-sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
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

	t.Run("QueryContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "SELECT * FROM query_test WHERE id = ?",
				Parameters: []interface{}{1},
				WantSpan:   nil,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)

			rows, err := db.QueryContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			if rows != nil {
				_ = rows.Close()
			}

			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
		}

		// `got` should be empty
		if len(got) != 0 {
			t.Errorf("got %d spans, want 0", len(got))
		}
	})

	t.Run("ExecContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT INTO exec_test (id, name) VALUES (?, ?)",
				Parameters: []interface{}{1, "John"},
				WantSpan:   nil,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(context.Background(), hub), 10*time.Second)

			_, err := db.ExecContext(ctx, tt.Query, tt.Parameters...)
			if err != nil && !tt.WantError {
				cancel()
				t.Fatal(err)
			}

			cancel()
		}

		if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
		close(spansCh)

		var got [][]*sentry.Span
		for e := range spansCh {
			got = append(got, e)
		}

		// `got` should be empty
		if len(got) != 0 {
			t.Errorf("got %d spans, want 0", len(got))
		}
	})
}
