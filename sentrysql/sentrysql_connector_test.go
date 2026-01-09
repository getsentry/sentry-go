package sentrysql_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/sentrysql"
	"github.com/google/go-cmp/cmp"
)

//nolint:dupl
func TestNewSentrySQLConnector_Integration(t *testing.T) {
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(&fakeConnector{}, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("fakedb")), sentrysql.WithDatabaseName("fake")))
	t.Cleanup(func() {
		_, _ = db.Exec("WIPE")
		_ = db.Close()
	})
	setupQueries := []string{
		"CREATE|exec_test|id=int32,name=string",
		"CREATE|query_test|id=int32,name=string,age=int32,created_at=string",
		"INSERT|query_test|id=1,name=John,age=30,created_at=2023-01-01",
		"INSERT|query_test|id=2,name=Jane,age=25,created_at=2023-01-02",
		"INSERT|query_test|id=3,name=Bob,age=35,created_at=2023-01-03",
	}

	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err := db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on fakedb: %v", err)
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
				Query:      "SELECT|query_test|id|id=?",
				Parameters: []interface{}{1},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "SELECT|query_test|id|id=?",
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
						"db.system.name":    sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":      "fake",
						"db.operation.name": "SELECT",
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{1, "John"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:     "CREATE|temporary_test|id=int32,name=string",
				WantError: false,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "CREATE|temporary_test|id=int32,name=string",
					Op:          "db.sql.execute",
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
func TestNewSentrySQLConnector_Conn(t *testing.T) {
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(&fakeConnector{}, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("fakedb")), sentrysql.WithDatabaseName("fake")))
	t.Cleanup(func() {
		_, _ = db.Exec("WIPE")
		_ = db.Close()
	})

	setupQueries := []string{
		"CREATE|exec_test|id=int32,name=string",
		"CREATE|query_test|id=int32,name=string,age=int32,created_at=string",
		"INSERT|query_test|id=1,name=John,age=30,created_at=2023-01-01",
		"INSERT|query_test|id=2,name=Jane,age=25,created_at=2023-01-02",
		"INSERT|query_test|id=3,name=Bob,age=35,created_at=2023-01-03",
	}
	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err := db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on fakedb: %v", err)
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
				Query:      "SELECT|query_test|id|id=?",
				Parameters: []interface{}{1},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "SELECT|query_test|id|id=?",
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
						"db.system.name":    sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":      "fake",
						"db.operation.name": "SELECT",
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{2, "Peter"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
func TestNewSentrySQLConnector_BeginTx(t *testing.T) {
	t.Skip("fakedb does not implement transactions")

	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(&fakeConnector{}, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("fakedb")), sentrysql.WithDatabaseName("fake")))
	t.Cleanup(func() {
		_, _ = db.Exec("WIPE")
		_ = db.Close()
	})

	setupQueries := []string{
		"CREATE|exec_test|id=int32,name=string",
		"CREATE|query_test|id=int32,name=string,age=int32,created_at=string",
		"INSERT|query_test|id=1,name=John,age=30,created_at=2023-01-01",
		"INSERT|query_test|id=2,name=Jane,age=25,created_at=2023-01-02",
		"INSERT|query_test|id=3,name=Bob,age=35,created_at=2023-01-03",
	}

	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err := db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on fakedb: %v", err)
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
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{2, "Peter"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
		err = tx.QueryRowContext(ctx, "SELECT|query_test|name|id=?", 1).Scan(&name)
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_, err = tx.ExecContext(ctx, "INSERT|exec_test|id=?,name=?", 5, "Catherine")
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
					"db.system.name": sentrysql.DatabaseSystem("fakedb"),
					"db.namespace":   "fake",
				},
				Description: "SELECT|query_test|name|id=?",
				Op:          "db.sql.query",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
			{
				Data: map[string]interface{}{
					"db.system.name": sentrysql.DatabaseSystem("fakedb"),
					"db.namespace":   "fake",
				},
				Description: "INSERT|exec_test|id=?,name=?",
				Op:          "db.sql.execute",
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
		err = tx.QueryRowContext(ctx, "SELECT|query_test|name|id=?", 1).Scan(&name)
		if err != nil {
			_ = tx.Rollback()
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}

		_, err = tx.ExecContext(ctx, "INSERT|exec_test|id=?,name=?", 5, "Catherine")
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
					"db.system.name": sentrysql.DatabaseSystem("fakedb"),
					"db.namespace":   "fake",
				},
				Description: "SELECT|query_test|name|id=?",
				Op:          "db.sql.query",
				Tags:        nil,
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
			{
				Data: map[string]interface{}{
					"db.system.name": sentrysql.DatabaseSystem("fakedb"),
					"db.namespace":   "fake",
				},
				Description: "INSERT|exec_test|id=?,name=?",
				Op:          "db.sql.execute",
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
func TestNewSentrySQLConnector_PrepareContext(t *testing.T) {
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(&fakeConnector{}, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("fakedb")), sentrysql.WithDatabaseName("fake")))
	t.Cleanup(func() {
		_, _ = db.Exec("WIPE")
		_ = db.Close()
	})

	setupQueries := []string{
		"CREATE|exec_test|id=int32,name=string",
		"CREATE|query_test|id=int32,name=string,age=int32,created_at=string",
		"INSERT|query_test|id=1,name=John,age=30,created_at=2023-01-01",
		"INSERT|query_test|id=2,name=Jane,age=25,created_at=2023-01-02",
		"INSERT|query_test|id=3,name=Bob,age=35,created_at=2023-01-03",
	}
	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err := db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on fakedb: %v", err)
		}
	}

	t.Run("Exec", func(t *testing.T) {
		t.Skip("fakedb does not implement Exec")

		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "INSERT|exec_test|id=?,name=?",
				Parameters: []interface{}{3, "Sarah"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "INSERT|exec_test|id=?,name=?",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "INSERT  exec_test (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{4, "John", "Doe", "John Doe"},
				WantError:  true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":    sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":      "fake",
						"db.operation.name": "INSERT",
					},
					Description: "INSERT INTO exec_test (id, name) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.execute",
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
			if err != nil && !tt.WantError {
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
		t.Skip("fakedb does not implement Query")

		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "SELECT|query_test|id,name,age|id=?",
				Parameters: []interface{}{2},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name": sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":   "fake",
					},
					Description: "SELECT|query_test|id,name,age|id=?",
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
						"db.system.name":    sentrysql.DatabaseSystem("fakedb"),
						"db.namespace":      "fake",
						"server.address":    "localhost",
						"server.port":       "5432",
						"db.operation.name": "SELECT",
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
			if err != nil && !tt.WantError {
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

			// if WantSpan is nil, yet we got some spans, it should be an error
			if tt.WantSpan == nil {
				t.Errorf("Expecting no spans, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

			// if WantSpan is not nil, we should have at least one span
			if len(gotSpans) == 0 {
				t.Errorf("Expecting at least one span, but got %d spans: %v", len(gotSpans), gotSpans)
				continue
			}

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
func TestNewSentrySQLConnector_NoParentSpan(t *testing.T) {
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(&fakeConnector{}, sentrysql.WithDatabaseSystem(sentrysql.DatabaseSystem("fakedb")), sentrysql.WithDatabaseName("fake")))
	t.Cleanup(func() {
		_, _ = db.Exec("WIPE")
		_ = db.Close()
	})

	setupQueries := []string{
		"CREATE|exec_test|id=int32,name=string",
		"CREATE|query_test|id=int32,name=string,age=int32,created_at=string",
		"INSERT|query_test|id=1,name=John,age=30,created_at=2023-01-01",
		"INSERT|query_test|id=2,name=Jane,age=25,created_at=2023-01-02",
		"INSERT|query_test|id=3,name=Bob,age=35,created_at=2023-01-03",
	}
	setupCtx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()

	for _, query := range setupQueries {
		_, err := db.ExecContext(setupCtx, query)
		if err != nil {
			t.Fatalf("initializing table on fakedb: %v", err)
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
				Query:      "SELECT|query_test|id,name,age|id=?",
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
				Query:      "INSERT|exec_test|id=?,name=?",
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
