package valkey

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/redis"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyhook"
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

// Options configures the Sentry valkey-go instrumentation hook.
type Options struct {
	// Type determines the Sentry Insights module.
	// TypeCache (default) reports cache.get / cache.put spans.
	// TypeDB reports db spans with scrubbed command descriptions.
	Type InstrumentationType
}

type sentryHook struct {
	typ redis.InstrumentationType
}

var _ valkeyhook.Hook = (*sentryHook)(nil)

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) Do(client valkey.Client, ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	cmds := cmd.Commands()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemValkey, cmds, cmd.IsReadOnly(), addr)
	defer span.Finish()

	result := client.Do(span.Context(), cmd)

	redis.FinishSpan(span, h.typ, cmd.IsReadOnly(), result.Error(), valkey.IsValkeyNil(result.Error()), respSize(result))
	return result
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) DoCache(client valkey.Client, ctx context.Context, cmd valkey.Cacheable, ttl time.Duration) valkey.ValkeyResult {
	cmds := cmd.Commands()
	completed := valkey.Completed(cmd)
	isReadOnly := completed.IsReadOnly()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemValkey, cmds, isReadOnly, addr)
	defer span.Finish()

	if h.typ == redis.TypeCache {
		span.SetData("cache.ttl", int(ttl.Seconds()))
	}

	result := client.DoCache(span.Context(), cmd, ttl)

	redis.FinishSpan(span, h.typ, isReadOnly, result.Error(), valkey.IsValkeyNil(result.Error()), respSize(result))
	return result
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) DoMulti(client valkey.Client, ctx context.Context, multi ...valkey.Completed) []valkey.ValkeyResult {
	addr := extractAddr(client)

	cmdsSlice := make([][]string, len(multi))
	for i, cmd := range multi {
		cmdsSlice[i] = cmd.Commands()
	}

	parentSpan := redis.StartPipelineSpan(ctx, h.typ, redis.DBSystemValkey, cmdsSlice, addr)
	defer parentSpan.Finish()

	results := client.DoMulti(parentSpan.Context(), multi...)

	hasError := false
	for i, cmd := range multi {
		cmds := cmd.Commands()
		childSpan := redis.StartChildSpan(parentSpan, h.typ, redis.DBSystemValkey, cmds, cmd.IsReadOnly(), addr)
		err := results[i].Error()
		isNil := valkey.IsValkeyNil(err)
		if err != nil && !isNil {
			hasError = true
		}
		redis.FinishSpan(childSpan, h.typ, cmd.IsReadOnly(), err, isNil, respSize(results[i]))
		childSpan.Finish()
	}

	redis.FinishPipelineSpan(parentSpan, hasError)
	return results
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) DoMultiCache(client valkey.Client, ctx context.Context, multi ...valkey.CacheableTTL) []valkey.ValkeyResult {
	addr := extractAddr(client)

	cmdsSlice := make([][]string, len(multi))
	for i, ct := range multi {
		cmdsSlice[i] = ct.Cmd.Commands()
	}

	parentSpan := redis.StartPipelineSpan(ctx, h.typ, redis.DBSystemValkey, cmdsSlice, addr)
	defer parentSpan.Finish()

	results := client.DoMultiCache(parentSpan.Context(), multi...)

	hasError := false
	for i, ct := range multi {
		cmds := ct.Cmd.Commands()
		completed := valkey.Completed(ct.Cmd)
		isReadOnly := completed.IsReadOnly()

		childSpan := redis.StartChildSpan(parentSpan, h.typ, redis.DBSystemValkey, cmds, isReadOnly, addr)
		if h.typ == redis.TypeCache {
			childSpan.SetData("cache.ttl", int(ct.TTL.Seconds()))
		}

		err := results[i].Error()
		isNil := valkey.IsValkeyNil(err)
		if err != nil && !isNil {
			hasError = true
		}
		redis.FinishSpan(childSpan, h.typ, isReadOnly, err, isNil, respSize(results[i]))
		childSpan.Finish()
	}

	redis.FinishPipelineSpan(parentSpan, hasError)
	return results
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) Receive(client valkey.Client, ctx context.Context, subscribe valkey.Completed, fn func(msg valkey.PubSubMessage)) error {
	cmds := subscribe.Commands()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemValkey, cmds, subscribe.IsReadOnly(), addr)
	defer span.Finish()

	err := client.Receive(span.Context(), subscribe, fn)

	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}
	return err
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) DoStream(client valkey.Client, ctx context.Context, cmd valkey.Completed) valkey.ValkeyResultStream {
	cmds := cmd.Commands()
	addr := extractAddr(client)

	span := redis.StartSpan(ctx, h.typ, redis.DBSystemValkey, cmds, cmd.IsReadOnly(), addr)

	stream := client.DoStream(span.Context(), cmd)

	if err := stream.Error(); err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}
	span.Finish()

	return stream
}

//nolint:revive // ctx position is dictated by valkeyhook.Hook interface.
func (h *sentryHook) DoMultiStream(client valkey.Client, ctx context.Context, multi ...valkey.Completed) valkey.MultiValkeyResultStream {
	addr := extractAddr(client)

	cmdsSlice := make([][]string, len(multi))
	for i, cmd := range multi {
		cmdsSlice[i] = cmd.Commands()
	}

	parentSpan := redis.StartPipelineSpan(ctx, h.typ, redis.DBSystemValkey, cmdsSlice, addr)

	stream := client.DoMultiStream(parentSpan.Context(), multi...)

	if err := stream.Error(); err != nil {
		parentSpan.Status = sentry.SpanStatusInternalError
	} else {
		parentSpan.Status = sentry.SpanStatusOK
	}
	parentSpan.Finish()

	return stream
}

// New creates a new valkey instrumentation hook using Sentry.
func New(options Options) valkeyhook.Hook {
	return &sentryHook{
		typ: options.Type,
	}
}

func extractAddr(client valkey.Client) redis.Address {
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

func respSize(result valkey.ValkeyResult) int {
	data, err := result.AsBytes()
	if err != nil {
		return 0
	}
	return len(data)
}
