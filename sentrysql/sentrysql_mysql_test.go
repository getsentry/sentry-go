package sentrysql_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DataDog/go-sqllexer"
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/sentrysql"
	mysqlclient "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/server"
	"github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"
)

// InMemoryMysqlHandler is a simple in-memory MySQL handler for testing purposes.
// It's basically a no-op handler that can respond to basic commands.
type InMemoryMysqlHandler struct{}

var _ server.Handler = (*InMemoryMysqlHandler)(nil)

// HandleFieldList handle COM_FILED_LIST command
func (i *InMemoryMysqlHandler) HandleFieldList(table string, fieldWildcard string) ([]*mysqlclient.Field, error) {
	return nil, nil
}

// HandleOtherCommand handle any other command that is not currently handled by the library,
// default implementation for this method will return an ER_UNKNOWN_ERROR
func (i *InMemoryMysqlHandler) HandleOtherCommand(cmd byte, data []byte) error {
	return nil
}

// HandleQuery handles COM_QUERY command, like SELECT, INSERT, UPDATE, etc...
// If Result has a Resultset (SELECT, SHOW, etc...), we will send this as the response, otherwise, we will send Result
func (i *InMemoryMysqlHandler) HandleQuery(query string) (*mysqlclient.Result, error) {
	normalizer := sqllexer.NewNormalizer(sqllexer.WithCollectCommands(true), sqllexer.WithCollectComments(true), sqllexer.WithCollectTables(true), sqllexer.WithKeepIdentifierQuotation(true))
	_, statementMetadata, err := normalizer.Normalize(query, sqllexer.WithDBMS(sqllexer.DBMSMySQL))
	if err != nil {
		return nil, err
	}

	command := strings.Join(statementMetadata.Commands, " ")
	switch command {
	case "SELECT":
		// return empty resultset
		return mysqlclient.NewResult(mysqlclient.NewResultset(0)), nil
	case "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER":
		// return OK result
		return nil, nil
	default:
		return nil, errors.New("unsupported command")
	}
}

// HandleStmtClose handle COM_STMT_CLOSE, context is the previous one set in prepare
// this handler has no response
func (i *InMemoryMysqlHandler) HandleStmtClose(context interface{}) error {
	return nil
}

// HandleStmtExecute handle COM_STMT_EXECUTE, context is the previous one set in prepare
// query is the statement prepare query, and args is the params for this statement
func (i *InMemoryMysqlHandler) HandleStmtExecute(context interface{}, query string, args []interface{}) (*mysqlclient.Result, error) {
	return nil, nil
}

// HandleStmtPrepare handle COM_STMT_PREPARE, params is the param number for this statement, columns is the column number
// context will be used later for statement execute
func (i *InMemoryMysqlHandler) HandleStmtPrepare(query string) (params int, columns int, context interface{}, err error) {
	// This is a very naive implementation just for testing purposes.
	// This function must not be a no-op and should return the correct number of params and columns.
	lexer := sqllexer.New(query, sqllexer.WithDBMS(sqllexer.DBMSMySQL))
	command := ""
	columnStop := false

	for {
		token := lexer.Scan()
		if token.Type == sqllexer.EOF {
			break
		}
		if command == "" && token.Type == sqllexer.COMMAND {
			command = token.Value
			continue
		}

		if token.Type == sqllexer.OPERATOR && token.Value == "?" {
			params += 1
			continue
		}

		if command != "" && token.Type == sqllexer.IDENT && !columnStop {
			columns += 1
			continue
		}

		if token.Type == sqllexer.WILDCARD && command == "SELECT" && !columnStop {
			columns += 1
			columnStop = true
			continue
		}

		if token.Type == sqllexer.KEYWORD && (token.Value == "FROM" || token.Value == "WHERE" || token.Value == "VALUES") {
			columnStop = true
		}
	}
	return params, columns, nil, nil
}

// UseDB handle COM_INIT_DB command, you can check whether the dbName is valid, or other.
func (i *InMemoryMysqlHandler) UseDB(dbName string) error {
	return nil
}

func TestNewSentrySQL_MySQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:13306")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
	})

	go func() {
		select {
		case <-ctx.Done():
			return
		default:
			// Accept a new connection once
			c, err := listener.Accept()
			if err != nil {
				t.Logf("failed to accept connection: %v", err)
				return
			}

			conn, err := server.NewDefaultServer().NewConn(c, "root", "password", &InMemoryMysqlHandler{})
			if err != nil {
				t.Logf("failed to create connection: %v", err)
				return
			}

			// as long as the client keeps sending commands, keep handling them
			for {
				if err := conn.HandleCommand(); err != nil {
					if err.Error() == "connection closed" {
						return
					}
					t.Logf("failed to handle command: %v", err)
				}
			}
		}
	}()

	config, err := mysql.ParseDSN("root:password@tcp(" + listener.Addr().String() + ")/")
	if err != nil {
		t.Fatalf("parsing mysql dsn: %v", err)
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatalf("creating mysql connector: %v", err)
	}

	host, port, _ := net.SplitHostPort(config.Addr)
	db := sql.OpenDB(sentrysql.NewSentrySQLConnector(connector, sentrysql.WithDatabaseSystem(sentrysql.MySQL), sentrysql.WithDatabaseName(config.DBName), sentrysql.WithServerAddress(host, port), sentrysql.AlwaysUseFallbackCommand()))
	t.Cleanup(func() {
		err := db.Close()
		if err != nil {
			t.Fatalf("closing mysql connection: %v", err)
		}
	})

	t.Run("QueryContext", func(t *testing.T) {
		tests := []struct {
			Query      string
			Parameters []interface{}
			WantSpan   *sentry.Span
			WantError  bool
		}{
			{
				Query:      "SELECT 1",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":    sentrysql.MySQL,
						"server.address":    "127.0.0.1",
						"server.port":       "13306",
						"db.operation.name": "SELECT",
						"db.query.text":     "SELECT ?",
					},
					Description: "SELECT ?",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "CREATE",
						"db.query.text":      "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
						"db.collection.name": "people",
					},
					Description: "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "INSERT INTO people (id, name, age, created_at) VALUES (1, 'Alice', 30, '2023-01-01'), (2, 'Bob', 25, '2023-01-02'), (3, 'Charlie', 28, '2023-01-03')",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "INSERT",
						"db.query.text":      "INSERT INTO people (id, name, age, created_at) VALUES (?), (?), (?)",
						"db.collection.name": "people",
					},
					Description: "INSERT INTO people (id, name, age, created_at) VALUES (?), (?), (?)",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "SELECT name FROM people WHERE id = ? AND foo = 'bar'; -- retrieve name by id",
				Parameters: []interface{}{20},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "SELECT",
						"db.query.text":      "SELECT name FROM people WHERE id = ? AND foo = ?",
						"db.collection.name": "people",
					},
					Description: "SELECT name FROM people WHERE id = ? AND foo = ?",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "SELECT * FROM people; -- Should not accept any parameters",
				Parameters: []interface{}{1, 2, 3, 4, 5},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "SELECT",
						"db.query.text":      "SELECT * FROM people",
						"db.collection.name": "people",
					},
					Description: "SELECT * FROM people",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
				},
				WantError: true,
			},
			{
				Query:      "SELECT name FROM people WHERE id = @Id",
				Parameters: []interface{}{sql.Named("Id", 10)},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "SELECT",
						"db.query.text":      "SELECT name FROM people WHERE id = @Id",
						"db.collection.name": "people",
					},
					Description: "SELECT name FROM people WHERE id = @Id",
					Op:          "db.sql.query",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusInternalError,
				},
				WantError: true,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(ctx, hub), 10*time.Second)
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
				Query:      "SELECT 1",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":    sentrysql.MySQL,
						"server.address":    "127.0.0.1",
						"server.port":       "13306",
						"db.operation.name": "SELECT",
						"db.query.text":     "SELECT ?",
					},
					Description: "SELECT ?",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "CREATE",
						"db.query.text":      "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
						"db.collection.name": "people",
					},
					Description: "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(?), age INT, created_at DATE)",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
			},
			{
				Query:      "INSERT INTO people (id, name, age, created_at) VALUES (1, 'Alice', 30, '2023-01-01'), (2, 'Bob', 25, '2023-01-02'), (3, 'Charlie', 28, '2023-01-03')",
				Parameters: []interface{}{},
				WantSpan: &sentry.Span{
					Data: map[string]interface{}{
						"db.system.name":     sentrysql.MySQL,
						"server.address":     "127.0.0.1",
						"server.port":        "13306",
						"db.operation.name":  "INSERT",
						"db.query.text":      "INSERT INTO people (id, name, age, created_at) VALUES (?), (?), (?)",
						"db.collection.name": "people",
					},
					Description: "INSERT INTO people (id, name, age, created_at) VALUES (?), (?), (?)",
					Op:          "db.sql.execute",
					Tags:        nil,
					Origin:      "manual",
					Sampled:     sentry.SampledTrue,
					Status:      sentry.SpanStatusOK,
				},
				WantError: false,
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
			ctx, cancel := context.WithTimeout(sentry.SetHubOnContext(ctx, hub), 10*time.Second)
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
}
