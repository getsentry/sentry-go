package sentry

import (
	"context"
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
	captured := requireSingleEvent(t, transport)

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
}

func TestCaptureObservesCurrentScopeContents(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTag("tag", "global")
	global.AddBreadcrumb(&Breadcrumb{Message: "global"}, defaultMaxBreadcrumbs)

	tests := []struct {
		name  string
		clear bool
	}{
		{name: "remove"}, {name: "clear", clear: true},
	}
	for _, test := range tests {
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
			captured := requireSingleEvent(t, transport)
			require.NotContains(t, captured.Tags, "tag")
			require.Empty(t, captured.Breadcrumbs)
		})
	}
}

func TestContextScopeReplacesGlobalScope(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTag("global", "value")

	client, transport := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: defaultMaxBreadcrumbs})
	ctx := ContextWithScope(context.Background(), NewScope())
	require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), NewEvent()))
	require.NotContains(t, requireSingleEvent(t, transport).Tags, "global")
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
			require.Equal(t, test.want, breadcrumbMessages(requireSingleEvent(t, transport).Breadcrumbs))
		})
	}
}

func TestCaptureRunsProcessorsFromGenericToSpecific(t *testing.T) {
	global := cleanGlobalScope(t)
	var order []string
	processor := func(name string) EventProcessor {
		return func(event *Event, _ *EventHint) *Event {
			order = append(order, name)
			return event
		}
	}
	global.AddEventProcessor(processor("global"))
	ctx, current := WithIsolationScope(context.Background())
	current.AddEventProcessor(processor("current"))
	data := Context{"value": "scope"}
	current.SetContext("custom", data)
	current.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		event.Contexts["custom"]["value"] = "event"
		return event
	})
	client, _ := newCaptureTestClient(t, ClientOptions{MaxBreadcrumbs: defaultMaxBreadcrumbs})
	client.AddEventProcessor(processor("client"))

	require.NotNil(t, CaptureEvent(ContextWithClient(ctx, client), NewEvent()))
	require.Equal(t, []string{"global", "current", "client"}, order)
	require.Equal(t, Context{"value": "scope"}, data)
}

func cleanGlobalScope(t testing.TB) *Scope {
	t.Helper()
	global := GlobalScope()
	original := global.Clone()
	originalLastEventID := global.lastEventIDSnapshot()
	global.mu.Lock()
	global.scopeData = newScopeDataWithPropagation(PropagationContext{})
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

func requireSingleEvent(t *testing.T, transport *MockTransport) *Event {
	t.Helper()
	events := transport.Events()
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
