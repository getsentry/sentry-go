package rueidis

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	librueidis "github.com/redis/rueidis"
	"github.com/redis/rueidis/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var tracingOpts = sentrytest.WithClientOptions(sentry.ClientOptions{
	EnableTracing:    true,
	TracesSampleRate: 1.0,
})

func setupMock(t *testing.T) (*mock.Client, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	client := mock.NewClient(ctrl)
	client.EXPECT().Nodes().Return(map[string]librueidis.Client{
		"localhost:6379": client,
	}).AnyTimes()
	return client, ctrl
}

func startTx(f *sentrytest.Fixture) *sentry.Span {
	ctx := sentry.SetHubOnContext(f.T.(*testing.T).Context(), f.Hub)
	return sentry.StartTransaction(ctx, "test")
}

func TestDo(t *testing.T) {
	tests := []struct {
		name       string
		typ        InstrumentationType
		buildCmd   func(b librueidis.Builder) librueidis.Completed
		result     librueidis.RedisResult
		wantSpans  int
		wantOp     string
		wantDesc   string
		wantStatus sentry.SpanStatus
		wantData   map[string]interface{}
	}{
		{
			name:       "DB mode SET",
			typ:        TypeDB,
			buildCmd:   func(b librueidis.Builder) librueidis.Completed { return b.Set().Key("mykey").Value("myvalue").Build() },
			result:     mock.Result(mock.RedisString("OK")),
			wantSpans:  1,
			wantOp:     "db.query",
			wantDesc:   "SET mykey ?",
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"db.system": "redis", "db.operation": "SET", "server.address": "localhost", "server.port": 6379},
		},
		{
			name:       "Cache GET hit",
			typ:        TypeCache,
			buildCmd:   func(b librueidis.Builder) librueidis.Completed { return b.Get().Key("session:abc").Build() },
			result:     mock.Result(mock.RedisBlobString("cached-value")),
			wantSpans:  1,
			wantOp:     "cache.get",
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.hit": true, "cache.success": true, "cache.key": []string{"session:abc"}},
		},
		{
			name:       "Cache GET miss",
			typ:        TypeCache,
			buildCmd:   func(b librueidis.Builder) librueidis.Completed { return b.Get().Key("missing").Build() },
			result:     mock.ErrorResult(librueidis.Nil),
			wantSpans:  1,
			wantOp:     "cache.get",
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.hit": false, "cache.success": true},
		},
		{
			name:       "DB error",
			typ:        TypeDB,
			buildCmd:   func(b librueidis.Builder) librueidis.Completed { return b.Get().Key("key").Build() },
			result:     mock.ErrorResult(errors.New("connection refused")),
			wantSpans:  1,
			wantOp:     "db.query",
			wantStatus: sentry.SpanStatusInternalError,
		},
		{
			name:       "Cache SET write",
			typ:        TypeCache,
			buildCmd:   func(b librueidis.Builder) librueidis.Completed { return b.Set().Key("key").Value("val").Build() },
			result:     mock.Result(mock.RedisString("OK")),
			wantSpans:  1,
			wantOp:     "cache.put",
			wantStatus: sentry.SpanStatusOK,
			wantData:   map[string]interface{}{"cache.write": true, "cache.success": true},
		},
		{
			name:      "Cache PING skipped",
			typ:       TypeCache,
			buildCmd:  func(b librueidis.Builder) librueidis.Completed { return b.Ping().Build() },
			result:    mock.Result(mock.RedisString("PONG")),
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				client, ctrl := setupMock(t)
				defer ctrl.Finish()

				client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(tt.result)

				hook := New(Options{Type: tt.typ})
				tx := startTx(f)
				cmd := tt.buildCmd(client.B())
				hook.Do(client, tx.Context(), cmd)
				tx.Finish()
				f.Flush()

				events := f.Events()
				require.Len(t, events, 1)
				require.Len(t, events[0].Spans, tt.wantSpans)

				if tt.wantSpans == 0 {
					return
				}
				s := events[0].Spans[0]
				assert.Equal(t, tt.wantOp, s.Op)
				assert.Equal(t, tt.wantStatus, s.Status)
				if tt.wantDesc != "" {
					assert.Equal(t, tt.wantDesc, s.Description)
				}
				for k, v := range tt.wantData {
					assert.Equal(t, v, s.Data[k], "span data %q", k)
				}
			}, tracingOpts)
		})
	}
}

func TestDoCache(t *testing.T) {
	tests := []struct {
		name     string
		typ      InstrumentationType
		ttl      time.Duration
		result   librueidis.RedisResult
		wantOp   string
		wantData map[string]interface{}
	}{
		{
			name:   "Cache mode sets TTL on hit",
			typ:    TypeCache,
			ttl:    time.Minute,
			result: mock.Result(mock.RedisBlobString("cached")),
			wantOp: "cache.get",
			wantData: map[string]interface{}{
				"cache.ttl": 60, "cache.hit": true, "cache.success": true,
			},
		},
		{
			name:   "DB mode omits cache attributes",
			typ:    TypeDB,
			ttl:    time.Minute,
			result: mock.Result(mock.RedisBlobString("value")),
			wantOp: "db.query",
			wantData: map[string]interface{}{
				"db.system": "redis",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				client, ctrl := setupMock(t)
				defer ctrl.Finish()

				client.EXPECT().DoCache(gomock.Any(), gomock.Any(), gomock.Any()).Return(tt.result)

				hook := New(Options{Type: tt.typ})
				tx := startTx(f)
				cmd := client.B().Get().Key("session:abc").Cache()
				hook.DoCache(client, tx.Context(), cmd, tt.ttl)
				tx.Finish()
				f.Flush()

				events := f.Events()
				require.Len(t, events, 1)
				require.Len(t, events[0].Spans, 1)

				s := events[0].Spans[0]
				assert.Equal(t, tt.wantOp, s.Op)
				for k, v := range tt.wantData {
					assert.Equal(t, v, s.Data[k], "span data %q", k)
				}
			}, tracingOpts)
		})
	}
}

func TestDoMulti(t *testing.T) {
	tests := []struct {
		name         string
		typ          InstrumentationType
		results      []librueidis.RedisResult
		wantSpans    int
		wantPipeOp   string
		wantPipeDesc string
		wantStatus   sentry.SpanStatus
	}{
		{
			name: "DB pipeline creates parent and children",
			typ:  TypeDB,
			results: []librueidis.RedisResult{
				mock.Result(mock.RedisString("OK")),
				mock.Result(mock.RedisBlobString("value")),
			},
			wantSpans:    3,
			wantPipeOp:   "db.query.pipeline",
			wantPipeDesc: "SET, GET",
			wantStatus:   sentry.SpanStatusOK,
		},
		{
			name: "pipeline with error",
			typ:  TypeDB,
			results: []librueidis.RedisResult{
				mock.ErrorResult(errors.New("WRONGTYPE")),
				mock.Result(mock.RedisBlobString("value")),
			},
			wantSpans:  3,
			wantPipeOp: "db.query.pipeline",
			wantStatus: sentry.SpanStatusInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				client, ctrl := setupMock(t)
				defer ctrl.Finish()

				client.EXPECT().DoMulti(gomock.Any(), gomock.Any(), gomock.Any()).Return(tt.results)

				hook := New(Options{Type: tt.typ})
				tx := startTx(f)
				cmds := []librueidis.Completed{
					client.B().Set().Key("k1").Value("v1").Build(),
					client.B().Get().Key("k2").Build(),
				}
				hook.DoMulti(client, tx.Context(), cmds...)
				tx.Finish()
				f.Flush()

				events := f.Events()
				require.Len(t, events, 1)
				require.Len(t, events[0].Spans, tt.wantSpans)

				var pipeSpan *sentry.Span
				for _, s := range events[0].Spans {
					if s.Op == tt.wantPipeOp {
						pipeSpan = s
						break
					}
				}
				require.NotNil(t, pipeSpan, "pipeline parent span")
				assert.Equal(t, tt.wantStatus, pipeSpan.Status)
				if tt.wantPipeDesc != "" {
					assert.Equal(t, tt.wantPipeDesc, pipeSpan.Description)
				}
			}, tracingOpts)
		})
	}
}

func TestExtractAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodes    map[string]librueidis.Client
		wantHost string
		wantPort int
	}{
		{
			name:     "parses host:port",
			nodes:    map[string]librueidis.Client{"10.0.0.1:6380": nil},
			wantHost: "10.0.0.1",
			wantPort: 6380,
		},
		{
			name:  "empty nodes",
			nodes: map[string]librueidis.Client{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)
			client.EXPECT().Nodes().Return(tt.nodes)
			addr := extractAddr(client)
			assert.Equal(t, tt.wantHost, addr.Host)
			assert.Equal(t, tt.wantPort, addr.Port)
		})
	}
}
