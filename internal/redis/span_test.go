package redis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/assert"
)

func TestSpanOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		typ        InstrumentationType
		cmds       []string
		isReadOnly bool
		isPipeline bool
		want       string
	}{
		{name: "DB single command", typ: TypeDB, cmds: []string{"GET", "key"}, isReadOnly: true, want: "db.query"},
		{name: "DB pipeline", typ: TypeDB, isPipeline: true, want: "db.query.pipeline"},
		{name: "Cache read", typ: TypeCache, cmds: []string{"GET", "key"}, isReadOnly: true, want: "cache.get"},
		{name: "Cache write", typ: TypeCache, cmds: []string{"SET", "key", "val"}, want: "cache.put"},
		{name: "Cache delete DEL", typ: TypeCache, cmds: []string{"DEL", "key"}, want: "cache.remove"},
		{name: "Cache delete UNLINK", typ: TypeCache, cmds: []string{"UNLINK", "key"}, want: "cache.remove"},
		{name: "Cache delete GETDEL", typ: TypeCache, cmds: []string{"GETDEL", "key"}, want: "cache.remove"},
		{name: "Cache flush FLUSHDB", typ: TypeCache, cmds: []string{"FLUSHDB"}, want: "cache.flush"},
		{name: "Cache flush FLUSHALL", typ: TypeCache, cmds: []string{"FLUSHALL"}, want: "cache.flush"},
		{name: "Cache pipeline", typ: TypeCache, isPipeline: true, want: "cache.pipeline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, spanOp(tt.typ, tt.cmds, tt.isReadOnly, tt.isPipeline))
		})
	}
}

func TestSpanOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		dbSys DBSystem
		typ   InstrumentationType
		want  sentry.SpanOrigin
	}{
		{name: "valkey DB", dbSys: DBSystemValkey, typ: TypeDB, want: "auto.db.valkey"},
		{name: "valkey Cache", dbSys: DBSystemValkey, typ: TypeCache, want: "auto.cache.valkey"},
		{name: "redis DB", dbSys: DBSystemRedis, typ: TypeDB, want: "auto.db.redis"},
		{name: "redis Cache", dbSys: DBSystemRedis, typ: TypeCache, want: "auto.cache.redis"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, spanOrigin(tt.dbSys, tt.typ))
		})
	}
}

// ctxWithPII returns a context with a Hub whose SendDefaultPII matches the flag.
func ctxWithPII(t *testing.T, sendPII bool) context.Context {
	t.Helper()
	if !sendPII {
		return t.Context()
	}
	client, _ := sentry.NewClient(sentry.ClientOptions{SendDefaultPII: true})
	hub := sentry.NewHub(client, sentry.NewScope())
	return sentry.SetHubOnContext(t.Context(), hub)
}

func TestSpanDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		typ            InstrumentationType
		cmds           []string
		sendDefaultPII bool
		want           string
	}{
		{name: "DB mode scrubs command", typ: TypeDB, cmds: []string{"SET", "mykey", "myvalue"}, want: "SET mykey ?"},
		{name: "Cache mode returns keys only", typ: TypeCache, cmds: []string{"GET", "session:abc"}, want: "session:abc"},
		{name: "Cache mode MGET comma-separated", typ: TypeCache, cmds: []string{"MGET", "k1", "k2", "k3"}, want: "k1, k2, k3"},
		{name: "DB mode HSET scrubs fields and values", typ: TypeDB, cmds: []string{"HSET", "user:1", "name", "Alice", "age", "30"}, want: "HSET user:1 ? ? ? ?"},
		{name: "Cache mode HSET returns only hash key", typ: TypeCache, cmds: []string{"HSET", "user:1", "name", "Alice"}, want: "user:1"},
		{name: "DB mode AUTH scrubs password", typ: TypeDB, cmds: []string{"AUTH", "supersecret"}, want: "AUTH ?"},

		// With SendDefaultPII, fields are preserved but values are still scrubbed.
		{name: "PII HSET preserves fields", typ: TypeDB, cmds: []string{"HSET", "user:1", "name", "Alice", "age", "30"}, sendDefaultPII: true, want: "HSET user:1 name ? age ?"},
		{name: "PII SET preserves flags", typ: TypeDB, cmds: []string{"SET", "mykey", "secret", "EX", "60"}, sendDefaultPII: true, want: "SET mykey ? EX 60"},
		{name: "PII AUTH still scrubs password", typ: TypeDB, cmds: []string{"AUTH", "supersecret"}, sendDefaultPII: true, want: "AUTH ?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := ctxWithPII(t, tt.sendDefaultPII)
			assert.Equal(t, tt.want, spanDescription(ctx, tt.typ, tt.cmds))
		})
	}
}

func TestJoinTruncated(t *testing.T) {
	t.Parallel()

	t.Run("short list", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "a, b, c", joinTruncated([]string{"a", "b", "c"}, ", "))
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", joinTruncated(nil, ", "))
	})

	t.Run("truncates long list", func(t *testing.T) {
		t.Parallel()
		items := make([]string, 50)
		for i := range items {
			items[i] = "longkeyname_" + string(rune('a'+i%26))
		}
		got := joinTruncated(items, ", ")
		assert.LessOrEqual(t, len(got), maxDescriptionLen+3, "output should be truncated")
		assert.True(t, strings.HasSuffix(got, "..."), "expected trailing '...'")
	})

	t.Run("accounts for separator length", func(t *testing.T) {
		t.Parallel()
		items := make([]string, 30)
		for i := range items {
			items[i] = "key"
		}
		got := joinTruncated(items, " | ")
		assert.LessOrEqual(t, len(got), maxDescriptionLen+3, "output should be truncated")
	})
}

func TestPipelineDescription(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("DB mode joins command names", func(t *testing.T) {
		t.Parallel()
		cmds := [][]string{
			{"SET", "k1", "v1"},
			{"GET", "k2"},
			{"DEL", "k3"},
		}
		assert.Equal(t, "SET, GET, DEL", pipelineDescription(ctx, TypeDB, cmds))
	})

	t.Run("Cache mode joins all keys", func(t *testing.T) {
		t.Parallel()
		cmds := [][]string{
			{"SET", "k1", "v1"},
			{"MGET", "k2", "k3"},
		}
		assert.Equal(t, "k1, k2, k3", pipelineDescription(ctx, TypeCache, cmds))
	})
}

var tracingOpts = sentrytest.WithClientOptions(sentry.ClientOptions{
	EnableTracing:    true,
	TracesSampleRate: 1.0,
})

func startTx(f *sentrytest.Fixture) *sentry.Span {
	ctx := sentry.SetHubOnContext(f.T.(*testing.T).Context(), f.Hub)
	return sentry.StartTransaction(ctx, "test")
}

func TestStartSpan_DB(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		tx := startTx(f)

		cmds := []string{"SET", "mykey", "myvalue"}
		addr := Address{Host: "localhost", Port: 6379}

		span := StartSpan(tx.Context(), TypeDB, DBSystemValkey, cmds, false, addr)
		span.Finish()
		tx.Finish()
		f.Flush()

		events := f.Events()
		assert.Len(t, events, 1, "event count")
		assert.Len(t, events[0].Spans, 1, "span count")

		s := events[0].Spans[0]
		assert.Equal(t, "db.query", s.Op)
		assert.Equal(t, "SET mykey ?", s.Description)
		assert.Equal(t, sentry.SpanOrigin("auto.db.valkey"), s.Origin)
		assert.Equal(t, "valkey", s.Data["db.system"])
		assert.Equal(t, "SET", s.Data["db.operation"])
		assert.Equal(t, "localhost", s.Data["server.address"])
		assert.Equal(t, 6379, s.Data["server.port"])
	}, tracingOpts)
}

func TestStartSpan_Cache(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		tx := startTx(f)

		cmds := []string{"GET", "session:abc"}
		addr := Address{Host: "10.0.0.1", Port: 6380}

		span := StartSpan(tx.Context(), TypeCache, DBSystemRedis, cmds, true, addr)
		span.Finish()
		tx.Finish()
		f.Flush()

		events := f.Events()
		assert.Len(t, events, 1, "event count")
		assert.Len(t, events[0].Spans, 1, "span count")

		s := events[0].Spans[0]
		assert.Equal(t, "cache.get", s.Op)
		assert.Equal(t, "session:abc", s.Description)
		assert.Equal(t, sentry.SpanOrigin("auto.cache.redis"), s.Origin)
		assert.Equal(t, []string{"session:abc"}, s.Data["cache.key"])
		assert.Equal(t, "10.0.0.1", s.Data["network.peer.address"])
		assert.Equal(t, 6380, s.Data["network.peer.port"])
	}, tracingOpts)
}

func TestFinishSpan(t *testing.T) {
	tests := []struct {
		name       string
		typ        InstrumentationType
		isReadOnly bool
		err        error
		isNilErr   bool
		itemSize   int
		wantStatus sentry.SpanStatus
		wantData   map[string]interface{}
	}{
		{
			name:       "cache read hit",
			typ:        TypeCache,
			isReadOnly: true,
			itemSize:   42,
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.hit": true, "cache.item_size": 42, "cache.success": true},
		},
		{
			name:       "cache read miss",
			typ:        TypeCache,
			isReadOnly: true,
			err:        errors.New("redis: nil"),
			isNilErr:   true,
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.hit": false, "cache.success": true},
		},
		{
			name:       "cache read error",
			typ:        TypeCache,
			isReadOnly: true,
			err:        errors.New("connection refused"),
			wantStatus: sentry.SpanStatusInternalError,
			wantData:   map[string]interface{}{"cache.success": false},
		},
		{
			name:       "cache write success",
			typ:        TypeCache,
			isReadOnly: false,
			itemSize:   100,
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.write": true, "cache.item_size": 100, "cache.success": true},
		},
		{
			name:       "cache write error",
			typ:        TypeCache,
			isReadOnly: false,
			err:        errors.New("connection refused"),
			wantStatus: sentry.SpanStatusInternalError,
			wantData:   map[string]interface{}{"cache.write": false, "cache.success": false},
		},
		{
			name:       "DB error",
			typ:        TypeDB,
			isReadOnly: false,
			err:        errors.New("connection refused"),
			wantStatus: sentry.SpanStatusInternalError,
			wantData:   map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				tx := startTx(f)

				cmds := []string{"SET", "key", "val"}
				if tt.isReadOnly {
					cmds = []string{"GET", "key"}
				}
				addr := Address{Host: "localhost", Port: 6379}
				span := StartSpan(tx.Context(), tt.typ, DBSystemValkey, cmds, tt.isReadOnly, addr)

				FinishSpan(span, tt.typ, tt.isReadOnly, tt.err, tt.isNilErr, tt.itemSize)
				span.Finish()
				tx.Finish()
				f.Flush()

				events := f.Events()
				assert.Len(t, events, 1)
				assert.Len(t, events[0].Spans, 1)

				s := events[0].Spans[0]
				assert.Equal(t, tt.wantStatus, s.Status)
				for k, v := range tt.wantData {
					assert.Equal(t, v, s.Data[k], "span data %q", k)
				}
			}, tracingOpts)
		})
	}
}

func TestStartPipelineSpan(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		tx := startTx(f)

		cmdsSlice := [][]string{
			{"SET", "k1", "v1"},
			{"GET", "k2"},
		}
		addr := Address{Host: "localhost", Port: 6379}

		parent := StartPipelineSpan(tx.Context(), TypeDB, DBSystemValkey, cmdsSlice, addr)

		child1 := StartChildSpan(parent, TypeDB, DBSystemValkey, cmdsSlice[0], false, addr)
		FinishSpan(child1, TypeDB, false, nil, false, 0)
		child1.Finish()

		child2 := StartChildSpan(parent, TypeDB, DBSystemValkey, cmdsSlice[1], true, addr)
		FinishSpan(child2, TypeDB, true, nil, false, 5)
		child2.Finish()

		FinishPipelineSpan(parent, false)
		parent.Finish()
		tx.Finish()
		f.Flush()

		events := f.Events()
		assert.Len(t, events, 1, "event count")
		assert.Len(t, events[0].Spans, 3, "1 parent + 2 children")

		var parentSpan *sentry.Span
		for _, s := range events[0].Spans {
			if s.Op == "db.query.pipeline" {
				parentSpan = s
				break
			}
		}
		assert.NotNil(t, parentSpan, "pipeline parent span")
		assert.Equal(t, "SET, GET", parentSpan.Description)
		assert.Equal(t, sentry.SpanStatusOK, parentSpan.Status)
		assert.Equal(t, "valkey", parentSpan.Data["db.system"])
	}, tracingOpts)
}
