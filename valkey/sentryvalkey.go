package valkey

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/redis"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyhook"
)

type Options redis.Options

type sentryHook struct {
	typ     redis.InstrumentationType
	timeout time.Duration
}

var _ valkeyhook.Hook = (*sentryHook)(nil)

// Do implements [valkeyhook.Hook].
func (s *sentryHook) Do(client valkey.Client, ctx context.Context, cmd valkey.Completed) (resp valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoCache implements [valkeyhook.Hook].
func (s *sentryHook) DoCache(client valkey.Client, ctx context.Context, cmd valkey.Cacheable, ttl time.Duration) (resp valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMulti implements [valkeyhook.Hook].
func (s *sentryHook) DoMulti(client valkey.Client, ctx context.Context, multi ...valkey.Completed) (resps []valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMultiCache implements [valkeyhook.Hook].
func (s *sentryHook) DoMultiCache(client valkey.Client, ctx context.Context, multi ...valkey.CacheableTTL) (resps []valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMultiStream implements [valkeyhook.Hook].
func (s *sentryHook) DoMultiStream(client valkey.Client, ctx context.Context, multi ...valkey.Completed) valkey.MultiValkeyResultStream {
	panic("unimplemented")
}

// DoStream implements [valkeyhook.Hook].
func (s *sentryHook) DoStream(client valkey.Client, ctx context.Context, cmd valkey.Completed) valkey.ValkeyResultStream {
	panic("unimplemented")
}

// Receive implements [valkeyhook.Hook].
func (s *sentryHook) Receive(client valkey.Client, ctx context.Context, subscribe valkey.Completed, fn func(msg valkey.PubSubMessage)) (err error) {
	panic("unimplemented")
}

// New creates a new rueidis instrumentation hook using Sentry
func New(options Options) valkeyhook.Hook {
	if options.Timeout == 0 {
		options.Timeout = redis.DefaultTimeout
	}

	return &sentryHook{
		typ:     options.Type,
		timeout: options.Timeout,
	}
}
