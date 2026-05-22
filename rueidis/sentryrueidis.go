package rueidis

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/redis"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidishook"
)

// InstrumentationType selects which Sentry Insights module the hook reports to.
type InstrumentationType = redis.InstrumentationType

const (
	// TypeCache reports spans to the Sentry Caches module.
	// This is the zero value and the default when Options is left empty.
	TypeCache = redis.TypeCache

	// TypeDB reports db spans with scrubbed command descriptions.
	TypeDB = redis.TypeDB
)

// Options configures the Sentry rueidis instrumentation hook.
type Options struct {
	// Type determines the Sentry Insights module.
	// TypeCache (default) reports cache.get / cache.put spans.
	// TypeDB reports db spans with scrubbed command descriptions.
	Type InstrumentationType
}

type sentryHook struct {
	typ redis.InstrumentationType
}

var _ rueidishook.Hook = (*sentryHook)(nil)

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) Do(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) rueidis.RedisResult {
	cmds := cmd.Commands()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemRedis, cmds, cmd.IsReadOnly(), addr)
	defer redis.FinishIfNotNil(span)

	result := client.Do(redis.SpanContext(ctx, span), cmd)

	redis.FinishSpan(span, h.typ, cmd.IsReadOnly(), result.Error(), rueidis.IsRedisNil(result.Error()), respSize(result))
	return result
}

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) DoCache(client rueidis.Client, ctx context.Context, cmd rueidis.Cacheable, ttl time.Duration) rueidis.RedisResult {
	cmds := cmd.Commands()
	completed := rueidis.Completed(cmd)
	isReadOnly := completed.IsReadOnly()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemRedis, cmds, isReadOnly, addr)
	defer redis.FinishIfNotNil(span)

	if span != nil && h.typ == redis.TypeCache {
		span.SetData("cache.ttl", int(ttl.Seconds()))
	}

	result := client.DoCache(redis.SpanContext(ctx, span), cmd, ttl)

	redis.FinishSpan(span, h.typ, isReadOnly, result.Error(), rueidis.IsRedisNil(result.Error()), respSize(result))
	return result
}

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) DoMulti(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) []rueidis.RedisResult {
	addr := extractAddr(client)

	cmdsSlice := make([][]string, len(multi))
	for i, cmd := range multi {
		cmdsSlice[i] = cmd.Commands()
	}

	parentSpan := redis.StartPipelineSpan(ctx, h.typ, redis.DBSystemRedis, cmdsSlice, addr)
	defer redis.FinishIfNotNil(parentSpan)

	results := client.DoMulti(redis.SpanContext(ctx, parentSpan), multi...)

	hasError := false
	for i, cmd := range multi {
		cmds := cmd.Commands()
		childSpan := redis.StartChildSpan(parentSpan, h.typ, redis.DBSystemRedis, cmds, cmd.IsReadOnly(), addr)
		err := results[i].Error()
		isNil := rueidis.IsRedisNil(err)
		if err != nil && !isNil {
			hasError = true
		}
		redis.FinishSpan(childSpan, h.typ, cmd.IsReadOnly(), err, isNil, respSize(results[i]))
		redis.FinishIfNotNil(childSpan)
	}

	redis.FinishPipelineSpan(parentSpan, hasError)
	return results
}

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) DoMultiCache(client rueidis.Client, ctx context.Context, multi ...rueidis.CacheableTTL) []rueidis.RedisResult {
	addr := extractAddr(client)

	cmdsSlice := make([][]string, len(multi))
	for i, ct := range multi {
		cmdsSlice[i] = ct.Cmd.Commands()
	}

	parentSpan := redis.StartPipelineSpan(ctx, h.typ, redis.DBSystemRedis, cmdsSlice, addr)
	defer redis.FinishIfNotNil(parentSpan)

	results := client.DoMultiCache(redis.SpanContext(ctx, parentSpan), multi...)

	hasError := false
	for i, ct := range multi {
		cmds := ct.Cmd.Commands()
		completed := rueidis.Completed(ct.Cmd)
		isReadOnly := completed.IsReadOnly()

		childSpan := redis.StartChildSpan(parentSpan, h.typ, redis.DBSystemRedis, cmds, isReadOnly, addr)
		if childSpan != nil && h.typ == redis.TypeCache {
			childSpan.SetData("cache.ttl", int(ct.TTL.Seconds()))
		}

		err := results[i].Error()
		isNil := rueidis.IsRedisNil(err)
		if err != nil && !isNil {
			hasError = true
		}
		redis.FinishSpan(childSpan, h.typ, isReadOnly, err, isNil, respSize(results[i]))
		redis.FinishIfNotNil(childSpan)
	}

	redis.FinishPipelineSpan(parentSpan, hasError)
	return results
}

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) Receive(client rueidis.Client, ctx context.Context, subscribe rueidis.Completed, fn func(msg rueidis.PubSubMessage)) error {
	cmds := subscribe.Commands()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemRedis, cmds, subscribe.IsReadOnly(), addr)
	defer redis.FinishIfNotNil(span)

	err := client.Receive(redis.SpanContext(ctx, span), subscribe, fn)

	if span != nil {
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
		} else {
			span.Status = sentry.SpanStatusOK
		}
	}
	return err
}

// DoStream and DoMultiStream are not instrumented because the span would be
// finished before the stream is consumed, resulting in misleading durations.

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) DoStream(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) rueidis.RedisResultStream {
	return client.DoStream(ctx, cmd)
}

//nolint:revive // ctx position is dictated by rueidishook.Hook interface.
func (h *sentryHook) DoMultiStream(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) rueidis.MultiRedisResultStream {
	return client.DoMultiStream(ctx, multi...)
}

// New creates a new rueidis instrumentation hook using Sentry.
func New(options Options) rueidishook.Hook {
	return &sentryHook{
		typ: options.Type,
	}
}

func extractAddr(client rueidis.Client) redis.Address {
	for addr := range client.Nodes() {
		h, p, err := net.SplitHostPort(addr)
		if err != nil {
			return redis.Address{Host: addr}
		}
		portNum, err := strconv.Atoi(p)
		if err != nil {
			return redis.Address{Host: h}
		}
		return redis.Address{Host: h, Port: portNum}
	}
	return redis.Address{}
}

func respSize(result rueidis.RedisResult) int {
	data, err := result.AsBytes()
	if err != nil {
		return 0
	}
	return len(data)
}
