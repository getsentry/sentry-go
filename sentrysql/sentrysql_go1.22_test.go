//go:build go1.22

package sentrysql_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	sqlq "github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/types"
	"github.com/dolthub/vitess/go/vt/proto/query"
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/sentrysql"
	"github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"
)

func createTestDatabase() *memory.DbProvider {
	db := memory.NewDatabase("test")
	db.BaseDatabase.EnablePrimaryKeyIndexes()

	pro := memory.NewDBProvider(db)
	session := memory.NewSession(sqlq.NewBaseSession(), pro)
	ctx := sqlq.NewContext(context.Background(), sqlq.WithSession(session))

	tableName := "users"
	table := memory.NewTable(db, "users", sqlq.NewPrimaryKeySchema(sqlq.Schema{
		{Name: "name", Type: types.Text, Nullable: false, Source: tableName, PrimaryKey: false},
		{Name: "email", Type: types.Text, Nullable: false, Source: tableName, PrimaryKey: true},
		{Name: "age", Type: types.Int32, Nullable: false, Source: tableName},
		{Name: "created_at", Type: types.MustCreateDatetimeType(query.Type_DATETIME, 6), Nullable: false, Source: tableName},
	}), db.GetForeignKeyCollection())
	db.AddTable(tableName, table)

	creationTime := time.Unix(0, 1667304000000001000).UTC()
	_ = table.Insert(ctx, sqlq.NewRow("Bob Smith", "bob@smith.com", 30, creationTime))
	_ = table.Insert(ctx, sqlq.NewRow("Jane Doe", "jane@doe.com", 25, creationTime))
	_ = table.Insert(ctx, sqlq.NewRow("John Doe", "john@doe.com", 35, creationTime))

	return pro
}

//nolint:dupl
func TestNewSentrySQLConnector_Go122(t *testing.T) {
	testDatabase := createTestDatabase()
	engine := sqle.NewDefault(testDatabase)
	session := memory.NewSession(sqlq.NewBaseSession(), testDatabase)
	ctx := sqlq.NewContext(context.Background(), sqlq.WithSession(session))
	ctx.SetCurrentDatabase("test")
	config := server.Config{
		Protocol: "tcp",
		Address:  fmt.Sprintf("%s:%d", "127.0.0.1", 3306),
	}
	s, err := server.NewServer(config, engine, memory.NewSessionBuilder(testDatabase), nil)
	if err != nil {
		t.Fatalf("creating new server: %s", err.Error())
	}

	go func() {
		if err = s.Start(); err != nil {
			panic(err)
		}
	}()
	defer func() {
		if err := s.Close(); err != nil {
			t.Logf("closing server connection: %s", err.Error())
		}
	}()

	connector, err := mysql.NewConnector(&mysql.Config{
		User:                     "root",
		Passwd:                   "",
		Net:                      "tcp",
		Addr:                     "127.0.0.1:3306",
		DBName:                   "test",
		AllowCleartextPasswords:  true,
		AllowFallbackToPlaintext: true,
		AllowNativePasswords:     true,
		ParseTime:                true,
	})
	if err != nil {
		t.Fatalf("creating mysql connector: %s", err.Error())
	}

	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(connector, sentrysql.WithDatabaseName("test"), sentrysql.WithDatabaseSystem(sentrysql.MySQL), sentrysql.WithServerAddress("127.0.0.1", "3306")))
	defer func() {
		_ = db.Close()
	}()

	t.Run("QueryContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query: "SELECT * FROM users",
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
						"db.operation":   "SELECT",
					},
					Description: "SELECT * FROM users",
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
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
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
				Query:      "INSERT INTO users (name, email, age, created_at) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{"Michael", "michael@example.com", 17, time.Now()},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO users (name, email, age, created_at) VALUES (?, ?, ?, ?)",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "UPDATE users SET name = ? WHERE email = ?",
				Parameters: []interface{}{"Michael Jordan", "michael@example.com"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
						"db.operation":   "UPDATE",
					},
					Description: "UPDATE users SET name = ? WHERE email = ?",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query:      "DELETE FROM users WHERE name = ?",
				Parameters: []interface{}{"Nolan"},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
						"db.operation":   "DELETE",
					},
					Description: "DELETE FROM users WHERE name = ?",
					Op:          "db.sql.exec",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
			},
			{
				Query: "INSERT INTO users (id, name) VALUES (?, ?, ?, ?)",
				Parameters: []interface{}{
					1, "John", "Doe", 1,
				},
				WantError: true,
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system":      sentrysql.MySQL,
						"db.name":        "test",
						"server.address": "127.0.0.1",
						"server.port":    "3306",
						"db.operation":   "INSERT",
					},
					Description: "INSERT INTO users (id, name) VALUES (?, ?, ?, ?)",
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
}
