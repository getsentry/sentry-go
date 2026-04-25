package rueidis

import (
	"context"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidishook"
	"time"
)

type SentryHook struct{}

var _ rueidishook.Hook = (*SentryHook)(nil)

// Do implements [rueidishook.Hook].
func (s *SentryHook) Do(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) (resp rueidis.RedisResult) {
	panic("unimplemented")
}

// DoCache implements [rueidishook.Hook].
func (s *SentryHook) DoCache(client rueidis.Client, ctx context.Context, cmd rueidis.Cacheable, ttl time.Duration) (resp rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMulti implements [rueidishook.Hook].
func (s *SentryHook) DoMulti(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) (resps []rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMultiCache implements [rueidishook.Hook].
func (s *SentryHook) DoMultiCache(client rueidis.Client, ctx context.Context, multi ...rueidis.CacheableTTL) (resps []rueidis.RedisResult) {
	panic("unimplemented")
}

// DoMultiStream implements [rueidishook.Hook].
func (s *SentryHook) DoMultiStream(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) rueidis.MultiRedisResultStream {
	panic("unimplemented")
}

// DoStream implements [rueidishook.Hook].
func (s *SentryHook) DoStream(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) rueidis.RedisResultStream {
	panic("unimplemented")
}

// Receive implements [rueidishook.Hook].
func (s *SentryHook) Receive(client rueidis.Client, ctx context.Context, subscribe rueidis.Completed, fn func(msg rueidis.PubSubMessage)) (err error) {
	panic("unimplemented")
}
