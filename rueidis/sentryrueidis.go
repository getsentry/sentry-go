package rueidis

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/redis"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidishook"
)

type Options redis.Options

type sentryHook struct {
	typ     redis.InstrumentationType
	timeout time.Duration
}

var _ rueidishook.Hook = (*sentryHook)(nil)

// Do implements [rueidishook.Hook].
func (s *sentryHook) Do(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) (resp rueidis.RedisResult) {
	panic("unimplemented")
}

// DoCache implements [rueidishook.Hook].
func (s *sentryHook) DoCache(client rueidis.Client, ctx context.Context, cmd rueidis.Cacheable, ttl time.Duration) (resp rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMulti implements [rueidishook.Hook].
func (s *sentryHook) DoMulti(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) (resps []rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMultiCache implements [rueidishook.Hook].
func (s *sentryHook) DoMultiCache(client rueidis.Client, ctx context.Context, multi ...rueidis.CacheableTTL) (resps []rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMultiStream implements [rueidishook.Hook].
func (s *sentryHook) DoMultiStream(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) rueidis.MultiRedisResultStream {
	panic("unimplemented")
}

// DoStream implements [rueidishook.Hook].
func (s *sentryHook) DoStream(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) rueidis.RedisResultStream {
	panic("unimplemented")
}

// Receive implements [rueidishook.Hook].
func (s *sentryHook) Receive(client rueidis.Client, ctx context.Context, subscribe rueidis.Completed, fn func(msg rueidis.PubSubMessage)) (err error) {
	panic("unimplemented")
}

// New creates a new rueidis instrumentation hook using Sentry
func New(options Options) rueidishook.Hook {
	if options.Timeout == 0 {
		options.Timeout = redis.DefaultTimeout
	}

	return &sentryHook{
		typ:     options.Type,
		timeout: options.Timeout,
	}
}
