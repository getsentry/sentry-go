package redis

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
)

const maxDescriptionLen = 200

// StartSpan creates a span for a single command, sets operation/description/origin/data.
// isReadOnly comes from the library's cmd.IsReadOnly() — determines cache.get vs cache.put.
func StartSpan(ctx context.Context, typ InstrumentationType, dbSys DBSystem, cmds []string, isReadOnly bool, addr Address) *sentry.Span {
	span := sentry.StartSpan(ctx, spanOp(typ, cmds, isReadOnly, false),
		sentry.WithDescription(spanDescription(ctx, typ, cmds)),
		sentry.WithSpanOrigin(spanOrigin(dbSys, typ)),
	)
	setSpanData(span, typ, dbSys, cmds, addr)
	return span
}

// StartPipelineSpan creates a parent span for a batch of commands.
func StartPipelineSpan(ctx context.Context, typ InstrumentationType, dbSys DBSystem, cmdsSlice [][]string, addr Address) *sentry.Span {
	span := sentry.StartSpan(ctx, spanOp(typ, nil, false, true),
		sentry.WithDescription(pipelineDescription(ctx, typ, cmdsSlice)),
		sentry.WithSpanOrigin(spanOrigin(dbSys, typ)),
	)
	setAddressData(span, typ, dbSys, addr)
	return span
}

// StartChildSpan creates a child span under a pipeline parent.
// isReadOnly comes from the library's cmd.IsReadOnly().
func StartChildSpan(parent *sentry.Span, typ InstrumentationType, dbSys DBSystem, cmds []string, isReadOnly bool, addr Address) *sentry.Span {
	ctx := parent.Context()
	span := sentry.StartSpan(ctx, spanOp(typ, cmds, isReadOnly, false),
		sentry.WithDescription(spanDescription(ctx, typ, cmds)),
		sentry.WithSpanOrigin(spanOrigin(dbSys, typ)),
	)
	setSpanData(span, typ, dbSys, cmds, addr)
	return span
}

// FinishSpan sets status and cache hit/miss metadata on the span.
// The caller is responsible for calling span.Finish().
// isReadOnly: from the library's cmd.IsReadOnly().
// isNilErr: whether the error is a "key not found" nil (ValkeyNil / RedisNil).
// itemSize: byte length of response data (used for cache.item_size on hits).
func FinishSpan(span *sentry.Span, typ InstrumentationType, isReadOnly bool, err error, isNilErr bool, itemSize int) {
	if err != nil && !isNilErr {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}

	if typ == TypeCache && isReadOnly {
		if isNilErr {
			span.SetData("cache.hit", false)
		} else if err == nil {
			span.SetData("cache.hit", true)
			if itemSize > 0 {
				span.SetData("cache.item_size", itemSize)
			}
		}
	}
}

// FinishPipelineSpan sets the parent pipeline span status.
// The caller is responsible for calling span.Finish().
// hasError should be true if any child command returned a real error (not nil-key).
func FinishPipelineSpan(span *sentry.Span, hasError bool) {
	if hasError {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = sentry.SpanStatusOK
	}
}

// spanOp returns the Sentry span operation string.
func spanOp(typ InstrumentationType, cmds []string, isReadOnly bool, isPipeline bool) string {
	switch typ {
	case TypeDB:
		if isPipeline {
			return "db.query.pipeline"
		}
		return "db.query"
	case TypeCache:
		if isPipeline {
			return "cache.pipeline"
		}
		if IsDeleteCommand(cmds) {
			return "cache.remove"
		}
		if isReadOnly {
			return "cache.get"
		}
		return "cache.put"
	}
	return "db.query"
}

// spanOrigin returns the appropriate span origin for the given db system and type.
func spanOrigin(dbSys DBSystem, typ InstrumentationType) sentry.SpanOrigin {
	if typ == TypeCache {
		return sentry.SpanOrigin("auto.cache." + string(dbSys))
	}
	return sentry.SpanOrigin("auto.db." + string(dbSys))
}

// sendDefaultPII extracts the SendDefaultPII flag from the context's hub.
func sendDefaultPII(ctx context.Context) bool {
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		if client := hub.Client(); client != nil {
			return client.Options().SendDefaultPII
		}
	}
	return false
}

// spanDescription builds the span description based on module type.
func spanDescription(ctx context.Context, typ InstrumentationType, cmds []string) string {
	switch typ {
	case TypeDB:
		return ScrubCommand(cmds, sendDefaultPII(ctx))
	case TypeCache:
		keys := ExtractKeys(cmds)
		return strings.Join(keys, ", ")
	}
	return ""
}

// setSpanData populates span data attributes based on the module type.
func setSpanData(span *sentry.Span, typ InstrumentationType, dbSys DBSystem, cmds []string, addr Address) {
	switch typ {
	case TypeDB:
		span.SetData("db.system", string(dbSys))
		if len(cmds) > 0 {
			span.SetData("db.operation", strings.ToUpper(cmds[0]))
		}
		span.SetData("server.address", addr.Host)
		if addr.Port > 0 {
			span.SetData("server.port", addr.Port)
		}
	case TypeCache:
		keys := ExtractKeys(cmds)
		if len(keys) > 0 {
			span.SetData("cache.key", keys)
		}
		span.SetData("network.peer.address", addr.Host)
		if addr.Port > 0 {
			span.SetData("network.peer.port", addr.Port)
		}
	}
}

// setAddressData sets only the address/port span data (for pipeline parent spans).
func setAddressData(span *sentry.Span, typ InstrumentationType, dbSys DBSystem, addr Address) {
	switch typ {
	case TypeDB:
		span.SetData("db.system", string(dbSys))
		span.SetData("server.address", addr.Host)
		if addr.Port > 0 {
			span.SetData("server.port", addr.Port)
		}
	case TypeCache:
		span.SetData("network.peer.address", addr.Host)
		if addr.Port > 0 {
			span.SetData("network.peer.port", addr.Port)
		}
	}
}

func pipelineDescription(ctx context.Context, typ InstrumentationType, cmdsSlice [][]string) string {
	switch typ {
	case TypeDB:
		names := make([]string, len(cmdsSlice))
		for i, cmds := range cmdsSlice {
			names[i] = CommandName(cmds)
		}
		return joinTruncated(names, ", ")
	case TypeCache:
		var allKeys []string
		for _, cmds := range cmdsSlice {
			allKeys = append(allKeys, ExtractKeys(cmds)...)
		}
		return joinTruncated(allKeys, ", ")
	}
	return ""
}

func joinTruncated(items []string, sep string) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			if b.Len()+len(sep)+len(item) > maxDescriptionLen {
				b.WriteString("...")
				break
			}
			b.WriteString(sep)
		} else if len(item) > maxDescriptionLen {
			b.WriteString("...")
			break
		}
		if b.Len()+len(item) > maxDescriptionLen {
			b.WriteString("...")
			break
		}
		b.WriteString(item)
	}
	return b.String()
}
