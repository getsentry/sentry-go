package sentry

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
)

func TestCaptureMergesIsolationScopeSnapshotAndEvent(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTags(map[string]string{"global": "global", "shared": "global"})
	global.SetContext("shared", Context{"source": "global"})
	global.SetUser(User{ID: "global"})
	global.SetFingerprint([]string{"global"})
	global.SetLevel(LevelWarning)
	global.AddBreadcrumb(&Breadcrumb{Message: "global"}, defaultMaxBreadcrumbs)
	global.AddAttachment(&Attachment{Filename: "global.txt"})

	ctx, current := WithIsolationScope(context.Background())
	current.SetTags(map[string]string{"current": "current", "shared": "current"})
	current.SetContext("shared", Context{"source": "current"})
	current.SetUser(User{ID: "current"})
	current.SetFingerprint([]string{"current"})
	current.SetLevel(LevelError)
	current.AddBreadcrumb(&Breadcrumb{Message: "current"}, defaultMaxBreadcrumbs)
	current.AddAttachment(&Attachment{Filename: "current.txt"})

	event := &Event{
		Tags:        map[string]string{"event": "event", "shared": "event"},
		Contexts:    map[string]Context{"shared": {"source": "event"}},
		User:        User{ID: "event"},
		Fingerprint: []string{"event"},
		Level:       LevelFatal,
		Breadcrumbs: []*Breadcrumb{{Message: "event"}},
		Attachments: []*Attachment{{Filename: "event.txt"}},
	}

	client, transport := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: defaultMaxBreadcrumbs})
	require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), event, WithLevel(LevelDebug)))
	captured := requireSingleEvent(t, client, transport)

	want := &Event{
		Tags:     map[string]string{"global": "global", "current": "current", "event": "event", "shared": "event"},
		Contexts: map[string]Context{"shared": {"source": "event"}},
		User:     User{ID: "event"}, Fingerprint: []string{"event"}, Level: LevelDebug,
		Breadcrumbs: []*Breadcrumb{{Message: "global"}, {Message: "current"}, {Message: "event"}},
		Attachments: []*Attachment{{Filename: "global.txt"}, {Filename: "current.txt"}, {Filename: "event.txt"}},
	}
	if diff := cmp.Diff(want, captured,
		cmpopts.IgnoreFields(Event{}, "EventID", "Timestamp", "Sdk", "sdkMetaData", "Platform", "Release", "ServerName"),
		cmpopts.IgnoreUnexported(Event{}), cmpopts.IgnoreFields(Breadcrumb{}, "Timestamp"),
		cmpopts.IgnoreMapEntries(func(key string, _ Context) bool { return key == traceContextKey }),
	); diff != "" {
		t.Error(diff)
	}

	t.Run("scope DSC follows matching trace and event DSC", func(t *testing.T) {
		traceID, spanID := TraceID{0xab}, SpanID{2}
		for _, test := range []struct {
			name, source string
			matching     bool
			uppercase    bool
			nativeSpanID SpanID
			eventDSC     DynamicSamplingContext
		}{
			{name: "typed", source: "typed", matching: true},
			{name: "string", source: "string", matching: true},
			{name: "string uppercase", source: "string", matching: true, uppercase: true},
			{name: "external", source: "external", matching: true},
			{name: "external foreign", source: "external"},
			{name: "external matching native span", source: "external", matching: true, nativeSpanID: spanID},
			{name: "external different native span", source: "external", matching: true, nativeSpanID: SpanID{3}},
			{name: "event", source: "typed", matching: true, eventDSC: DynamicSamplingContext{Entries: map[string]string{"sampled": "false"}}},
			{name: "frozen empty event", source: "typed", matching: true, eventDSC: DynamicSamplingContext{Frozen: true}},
		} {
			t.Run(test.name, func(t *testing.T) {
				dscTraceID := traceID
				if !test.matching {
					dscTraceID = TraceID{3}
				}
				dsc := DynamicSamplingContext{Frozen: true, Entries: map[string]string{traceIDContextKey: dscTraceID.String(), "public_key": "public"}}
				if test.uppercase {
					dsc.Entries[traceIDContextKey] = strings.ToUpper(dsc.Entries[traceIDContextKey])
				}
				scope := NewScope()
				scope.SetPropagationContext(PropagationContext{TraceID: dscTraceID, SpanID: spanID, DynamicSamplingContext: dsc})
				client, transport := newCaptureTestClient(t, ClientOptions{})
				ctx := ContextWithClient(ContextWithScope(context.Background(), scope), client)
				event := &Event{sdkMetaData: SDKMetaData{dsc: test.eventDSC}}
				var native *Span
				if test.nativeSpanID != zeroSpanID {
					native = &Span{TraceID: traceID, SpanID: test.nativeSpanID, Op: "db.query", Description: "read users", Status: SpanStatusOK, Data: map[string]interface{}{"query": "users"}}
					scope.setSpan(native)
					ctx = context.WithValue(ctx, spanContextKey{}, native)
				}
				switch test.source {
				case "typed":
					event.Contexts = map[string]Context{traceContextKey: {traceIDContextKey: traceID, spanIDContextKey: spanID}}
				case "string":
					event.Contexts = map[string]Context{traceContextKey: {traceIDContextKey: traceID.String(), spanIDContextKey: spanID.String()}}
				case "external":
					client.externalTraceResolver = testExternalResolverFunc(func(context.Context) (TraceID, SpanID, Sampled, bool) { return traceID, spanID, SampledUndefined, true })
				}
				require.NotNil(t, CaptureEvent(ctx, event))
				captured := requireSingleEvent(t, client, transport)
				if native != nil {
					want := Context{traceIDContextKey: traceID.String(), spanIDContextKey: spanID.String()}
					if native.SpanID == spanID {
						want = native.traceContext().Map()
					}
					require.Equal(t, jsonContext(t, want), captured.Contexts[traceContextKey])
				}
				require.Equal(t, traceID.String(), fmt.Sprint(captured.Contexts[traceContextKey][traceIDContextKey]))
				switch {
				case test.eventDSC.HasEntries() || test.eventDSC.IsFrozen():
					require.Equal(t, test.eventDSC.Entries, capturedTrace(t, transport, captured))
				case test.matching:
					require.Equal(t, dsc.Entries, capturedTrace(t, transport, captured))
				default:
					require.Empty(t, capturedTrace(t, transport, captured))
				}
			})
		}
	})

	t.Run("explicit scope and event trace contexts win", func(t *testing.T) {
		for _, source := range []string{"propagation", "native", "external"} {
			t.Run(source, func(t *testing.T) {
				client, transport := newCaptureTestClient(t, ClientOptions{})
				scope := NewScope()
				custom := Context{traceIDContextKey: TraceID{1}.String(), spanIDContextKey: SpanID{1}.String(), "op": "custom"}
				scope.SetContext(traceContextKey, custom)
				ctx := ContextWithClient(ContextWithScope(context.Background(), scope), client)
				switch source {
				case "propagation":
					scope.SetPropagationContext(PropagationContext{TraceID: TraceID{2}, SpanID: SpanID{2}})
				case "native":
					scope.setSpan(&Span{TraceID: TraceID{3}, SpanID: SpanID{3}})
				case "external":
					client.externalTraceResolver = testExternalResolverFunc(func(context.Context) (TraceID, SpanID, Sampled, bool) {
						return TraceID{3}, SpanID{3}, SampledUndefined, true
					})
				}
				require.NotNil(t, CaptureMessage(ctx, "scope trace"))
				require.Equal(t, custom, requireSingleEvent(t, client, transport).Contexts[traceContextKey])
				explicit := Context{traceIDContextKey: TraceID{4}.String(), spanIDContextKey: SpanID{4}.String()}
				for _, eventType := range []string{"", transactionType} {
					require.NotNil(t, CaptureEvent(ctx, &Event{Type: eventType, Contexts: map[string]Context{traceContextKey: explicit}}))
				}
				for _, event := range capturedEvents(t, client, transport)[1:] {
					require.Equal(t, explicit, event.Contexts[traceContextKey])
				}
			})
		}
	})
}

func TestCaptureObservesCurrentScopeContents(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTag("tag", "global")
	global.AddBreadcrumb(&Breadcrumb{Message: "global"}, defaultMaxBreadcrumbs)

	for _, test := range []struct {
		name  string
		clear bool
	}{
		{"remove", false}, {"clear", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, current := WithIsolationScope(context.Background())
			current.SetTag("tag", "current")
			current.AddBreadcrumb(&Breadcrumb{Message: "current"}, defaultMaxBreadcrumbs)
			if test.clear {
				current.Clear()
			} else {
				current.RemoveTag("tag")
				current.ClearBreadcrumbs()
			}
			client, transport := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: defaultMaxBreadcrumbs})
			require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), NewEvent()))
			captured := requireSingleEvent(t, client, transport)
			require.NotContains(t, captured.Tags, "tag")
			require.Empty(t, captured.Breadcrumbs)
		})
	}
	t.Run("explicit scope replaces global", func(t *testing.T) {
		client, transport := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: defaultMaxBreadcrumbs})
		ctx := ContextWithClient(ContextWithScope(context.Background(), NewScope()), client)
		require.NotNil(t, CaptureEvent(ctx, NewEvent()))
		captured := requireSingleEvent(t, client, transport)
		require.NotContains(t, captured.Tags, "tag")
		require.Empty(t, captured.Breadcrumbs)
	})
}

func TestCaptureBreadcrumbOrderAndLimit(t *testing.T) {
	global := cleanGlobalScope(t)
	global.AddBreadcrumb(&Breadcrumb{Message: "global-1", Timestamp: testNow.Add(4 * time.Hour)}, defaultMaxBreadcrumbs)
	global.AddBreadcrumb(&Breadcrumb{Message: "global-2", Timestamp: testNow.Add(3 * time.Hour)}, defaultMaxBreadcrumbs)

	ctx, current := WithIsolationScope(context.Background())
	current.AddBreadcrumb(&Breadcrumb{Message: "current", Timestamp: testNow.Add(2 * time.Hour)}, defaultMaxBreadcrumbs)

	for _, test := range []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "ordered", limit: 10, want: []string{"global-1", "global-2", "current", "event"}},
		{name: "trim generic first", limit: 2, want: []string{"current", "event"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := &Event{Breadcrumbs: []*Breadcrumb{{Message: "event", Timestamp: testNow.Add(time.Hour)}}}
			client, transport := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: test.limit})
			require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), event))
			require.Equal(t, test.want, breadcrumbMessages(requireSingleEvent(t, client, transport).Breadcrumbs))
		})
	}
}

func TestCaptureRunsProcessorsFromGenericToSpecific(t *testing.T) {
	global := cleanGlobalScope(t)
	previousProcessors := globalEventProcessors
	globalEventProcessors = nil
	t.Cleanup(func() { globalEventProcessors = previousProcessors })
	var order []string
	processor := func(name string, want Level) EventProcessor {
		return func(event *Event, _ *EventHint) *Event {
			require.Equal(t, want, event.Level, name)
			order = append(order, name)
			return event
		}
	}
	global.AddEventProcessor(processor("global scope", LevelDebug))
	ctx, current := WithIsolationScope(context.Background())
	current.SetLevel(LevelError)
	current.AddEventProcessor(processor("current scope", LevelDebug))
	data := Context{"value": "scope"}
	current.SetContext("custom", data)
	current.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		event.Contexts["custom"]["value"] = "event"
		event.Level = LevelFatal
		return event
	})
	client, transport := newCaptureTestClient(t, ClientOptions{
		MaxBreadcrumbs: defaultMaxBreadcrumbs,
		BeforeSend:     processor("before send", LevelFatal),
	})
	client.AddEventProcessor(processor("client", LevelFatal))
	AddGlobalEventProcessor(processor("global processor", LevelFatal))

	require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), NewEvent(), WithLevel(LevelDebug)))
	require.Equal(t, []string{"global scope", "current scope", "client", "global processor", "before send"}, order)
	require.Equal(t, LevelFatal, requireSingleEvent(t, client, transport).Level)
	require.Equal(t, Context{"value": "scope"}, data)
}

func cleanGlobalScope(t testing.TB) *Scope {
	t.Helper()
	global := GlobalScope()
	original := global.Clone()
	originalLastEventID := global.lastEventIDSnapshot()
	global.mu.Lock()
	global.scopeData = newScopeData(PropagationContext{})
	global.eventProcessors = nil
	global.mu.Unlock()
	t.Cleanup(func() {
		global.mu.Lock()
		global.scopeData = original.scopeData
		global.eventProcessors = original.eventProcessors
		global.mu.Unlock()
		global.setLastEventID(originalLastEventID)
	})
	return global
}

func requireSingleEvent(t *testing.T, client *Client, transport *MockTransport) *Event {
	t.Helper()
	events := capturedEvents(t, client, transport)
	require.Len(t, events, 1)
	return events[0]
}

func breadcrumbMessages(breadcrumbs []*Breadcrumb) []string {
	messages := make([]string, len(breadcrumbs))
	for i, breadcrumb := range breadcrumbs {
		messages[i] = breadcrumb.Message
	}
	return messages
}
