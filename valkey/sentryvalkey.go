package valkey

import (
	"context"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyhook"
	"time"
)

type SentryHook struct{}

var _ valkeyhook.Hook = (*SentryHook)(nil)

// Do implements [valkeyhook.Hook].
func (s *SentryHook) Do(client valkey.Client, ctx context.Context, cmd valkey.Completed) (resp valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoCache implements [valkeyhook.Hook].
func (s *SentryHook) DoCache(client valkey.Client, ctx context.Context, cmd valkey.Cacheable, ttl time.Duration) (resp valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMulti implements [valkeyhook.Hook].
func (s *SentryHook) DoMulti(client valkey.Client, ctx context.Context, multi ...valkey.Completed) (resps []valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMultiCache implements [valkeyhook.Hook].
func (s *SentryHook) DoMultiCache(client valkey.Client, ctx context.Context, multi ...valkey.CacheableTTL) (resps []valkey.ValkeyResult) {
	panic("unimplemented")
}

// DoMultiStream implements [valkeyhook.Hook].
func (s *SentryHook) DoMultiStream(client valkey.Client, ctx context.Context, multi ...valkey.Completed) valkey.MultiValkeyResultStream {
	panic("unimplemented")
}

// DoStream implements [valkeyhook.Hook].
func (s *SentryHook) DoStream(client valkey.Client, ctx context.Context, cmd valkey.Completed) valkey.ValkeyResultStream {
	panic("unimplemented")
}

// Receive implements [valkeyhook.Hook].
func (s *SentryHook) Receive(client valkey.Client, ctx context.Context, subscribe valkey.Completed, fn func(msg valkey.PubSubMessage)) (err error) {
	panic("unimplemented")
}
