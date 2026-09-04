package sentry

import (
	"context"
	"slices"
)

// applyScopeToEvent applies the effective scope to event and returns the scope
// processors so they can run after the scope lock is released.
func applyScopeToEvent(
	ctx context.Context,
	event *Event,
	scope *Scope,
	client *Client,
	maxBreadcrumbs int,
) []EventProcessor {
	_, explicitTrace := event.Contexts[traceContextKey]

	scope.mu.RLock()
	cloneContexts := len(scope.eventProcessors) > 0 ||
		len(client.eventProcessors) > 0 ||
		len(globalEventProcessors) > 0 ||
		(event.Type == transactionType && client.options.BeforeSendTransaction != nil) ||
		(event.Type != transactionType && event.Type != checkInType && client.options.BeforeSend != nil)
	// Keep the common case on the stack while remembering which contexts came
	// from the scope. Only those maps need isolation from event processors.
	var inlineContextKeys [8]string
	contextKeysToClone := inlineContextKeys[:0]
	if len(scope.tags) > 0 {
		if event.Tags == nil {
			event.Tags = make(map[string]string, len(scope.tags))
		}
		for key, value := range scope.tags {
			if _, exists := event.Tags[key]; !exists {
				event.Tags[key] = value
			}
		}
	}
	if len(scope.contexts) > 0 {
		if event.Contexts == nil {
			event.Contexts = make(map[string]Context, len(scope.contexts))
		}
		for key, value := range scope.contexts {
			if key == traceContextKey && event.Type == transactionType {
				continue
			}
			if _, exists := event.Contexts[key]; !exists {
				event.Contexts[key] = value
				if cloneContexts {
					contextKeysToClone = append(contextKeysToClone, key)
				}
			}
		}
	}

	// Copy slice-backed scope data while holding the read lock. Snapshotting
	// only the slice headers would race with concurrent append operations.
	event.Breadcrumbs = mergeBreadcrumbs(scope.breadcrumbs, event.Breadcrumbs, maxBreadcrumbs)
	event.Attachments = prependSlice(scope.attachments, event.Attachments)
	if event.User.IsEmpty() && !scope.user.IsEmpty() {
		event.User = scope.user
	}
	if len(event.Fingerprint) == 0 && len(scope.fingerprint) > 0 {
		event.Fingerprint = slices.Clone(scope.fingerprint)
	}
	if event.Level == "" {
		event.Level = scope.level
	}

	request := scope.request
	requestBody := scope.requestBody
	propagationContext := scope.propagationContext
	processors := scope.eventProcessors[:len(scope.eventProcessors):len(scope.eventProcessors)]
	scope.mu.RUnlock()

	for _, key := range contextKeysToClone {
		event.Contexts[key] = cloneContext(event.Contexts[key])
	}

	if event.Request == nil && request != nil {
		event.Request = newRequest(request, client)
		dc := client.GetDataCollection()
		if requestBody != nil && !requestBody.Overflow() && dc.CollectHTTPBody(BodyIncomingRequest) {
			event.Request.Data = dc.FilterHTTPBody(
				requestBody.Bytes(),
				request.Header.Get("Content-Type"),
			)
		}
	}

	applyTraceToEvent(ctx, event, client, propagationContext, explicitTrace)
	return processors
}

func applyTraceToEvent(
	ctx context.Context,
	event *Event,
	client *Client,
	propagationContext PropagationContext,
	explicit bool,
) {
	if explicit || event.Type == transactionType {
		return
	}

	trace := activeTraceFromContexts(client, ctx)
	if trace.span != nil {
		setEventTrace(event, trace.span.traceContext().Map())
		if !event.sdkMetaData.dsc.HasEntries() && !event.sdkMetaData.dsc.IsFrozen() {
			if transaction := trace.span.GetTransaction(); transaction != nil {
				event.sdkMetaData.dsc = dynamicSamplingContextFromTransaction(transaction, client)
			}
		}
		return
	}
	if trace.traceID != zeroTraceID {
		setEventTrace(event, Context{
			traceIDContextKey: trace.traceID.String(),
			spanIDContextKey:  trace.spanID.String(),
		})
		return
	}
	if propagationContext.TraceID == zeroTraceID {
		return
	}

	setEventTrace(event, propagationContext.Map())
	if !event.sdkMetaData.dsc.HasEntries() && !event.sdkMetaData.dsc.IsFrozen() {
		dsc := propagationContext.DynamicSamplingContext
		if !dsc.HasEntries() {
			dsc = dynamicSamplingContextFromPropagationContext(propagationContext, client)
		}
		event.sdkMetaData.dsc = dsc
	}
}

func setEventTrace(event *Event, trace Context) {
	if event.Contexts == nil {
		event.Contexts = make(map[string]Context)
	}
	event.Contexts[traceContextKey] = trace
}

func mergeBreadcrumbs(scope, event []*Breadcrumb, limit int) []*Breadcrumb {
	switch {
	case limit < 0:
		return nil
	case limit == 0:
		limit = defaultMaxBreadcrumbs
	}

	event = event[max(0, len(event)-limit):]
	scopeLimit := limit - len(event)
	scope = scope[max(0, len(scope)-scopeLimit):]
	return prependSlice(scope, event)
}

func prependSlice[T any](prefix, suffix []T) []T {
	if len(prefix) == 0 {
		return suffix
	}
	merged := make([]T, len(prefix)+len(suffix))
	copy(merged, prefix)
	copy(merged[len(prefix):], suffix)
	return merged
}
