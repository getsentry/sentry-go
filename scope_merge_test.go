package sentry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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

	client, transport := newScopeMergeTestClient(t, defaultMaxBreadcrumbs)
	require.NotNil(t, client.captureEvent(
		ctx,
		event,
		WithLevel(LevelDebug),
	))
	captured := requireSingleEvent(t, transport)

	if diff := cmp.Diff(map[string]string{
		"global":  "global",
		"current": "current",
		"event":   "event",
		"shared":  "event",
	}, captured.Tags); diff != "" {
		t.Errorf("tags mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(Context{"source": "event"}, captured.Contexts["shared"]); diff != "" {
		t.Errorf("context mismatch (-want +got):\n%s", diff)
	}
	require.Equal(t, User{ID: "event"}, captured.User)
	require.Equal(t, []string{"event"}, captured.Fingerprint)
	require.Equal(t, LevelDebug, captured.Level)
	require.Equal(t, []string{"global", "current", "event"}, breadcrumbMessages(captured.Breadcrumbs))
	require.Equal(t, []string{"global.txt", "current.txt", "event.txt"}, attachmentNames(captured.Attachments))
}

func TestCaptureObservesCurrentScopeContents(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTag("tag", "global")
	global.AddBreadcrumb(&Breadcrumb{Message: "global"}, defaultMaxBreadcrumbs)

	tests := []struct {
		name   string
		mutate func(*Scope)
	}{
		{
			name: "remove",
			mutate: func(scope *Scope) {
				scope.SetTag("tag", "current")
				scope.RemoveTag("tag")
				scope.AddBreadcrumb(&Breadcrumb{Message: "current"}, defaultMaxBreadcrumbs)
				scope.ClearBreadcrumbs()
			},
		},
		{
			name: "clear",
			mutate: func(scope *Scope) {
				scope.SetTag("tag", "current")
				scope.AddBreadcrumb(&Breadcrumb{Message: "current"}, defaultMaxBreadcrumbs)
				scope.Clear()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, current := WithIsolationScope(context.Background())
			test.mutate(current)
			client, transport := newScopeMergeTestClient(t, defaultMaxBreadcrumbs)
			require.NotNil(t, client.captureEvent(
				ctx,
				NewEvent(),
			))
			captured := requireSingleEvent(t, transport)
			require.NotContains(t, captured.Tags, "tag")
			require.Empty(t, captured.Breadcrumbs)
		})
	}
}

func TestContextScopeReplacesGlobalScope(t *testing.T) {
	global := cleanGlobalScope(t)
	global.SetTag("global", "value")

	client, transport := newScopeMergeTestClient(t, defaultMaxBreadcrumbs)
	ctx := ContextWithScope(context.Background(), NewScope())
	require.NotNil(t, client.captureEvent(ctx, NewEvent()))
	require.NotContains(t, requireSingleEvent(t, transport).Tags, "global")
}

func TestCaptureBreadcrumbOrderAndLimit(t *testing.T) {
	global := cleanGlobalScope(t)
	global.AddBreadcrumb(&Breadcrumb{Message: "global-1", Timestamp: testNow.Add(4 * time.Hour)}, defaultMaxBreadcrumbs)
	global.AddBreadcrumb(&Breadcrumb{Message: "global-2", Timestamp: testNow.Add(3 * time.Hour)}, defaultMaxBreadcrumbs)

	ctx, current := WithIsolationScope(context.Background())
	current.AddBreadcrumb(&Breadcrumb{Message: "current", Timestamp: testNow.Add(2 * time.Hour)}, defaultMaxBreadcrumbs)
	event := &Event{Breadcrumbs: []*Breadcrumb{{Message: "event", Timestamp: testNow.Add(time.Hour)}}}

	for _, test := range []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "ordered", limit: 10, want: []string{"global-1", "global-2", "current", "event"}},
		{name: "trim generic first", limit: 2, want: []string{"current", "event"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newScopeMergeTestClient(t, test.limit)
			require.NotNil(t, client.captureEvent(
				ctx,
				event,
			))
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
	client, _ := newScopeMergeTestClient(t, defaultMaxBreadcrumbs)
	client.AddEventProcessor(processor("client"))

	require.NotNil(t, client.captureEvent(ctx, NewEvent()))
	require.Equal(t, []string{"global", "current", "client"}, order)
}

func TestCaptureMergeSerializationSafety(t *testing.T) {
	global := cleanGlobalScope(t)
	globalContext := Context{"value": "global-before"}
	globalBreadcrumbData := map[string]any{"value": "global-before"}
	global.SetContext("global", globalContext)
	global.AddBreadcrumb(&Breadcrumb{Message: "global", Data: globalBreadcrumbData}, defaultMaxBreadcrumbs)

	ctx, current := WithIsolationScope(context.Background())
	currentContext := Context{"value": "current-before"}
	currentBreadcrumbData := map[string]any{"value": "current-before"}
	current.SetContext("current", currentContext)
	current.AddBreadcrumb(&Breadcrumb{Message: "current", Data: currentBreadcrumbData}, defaultMaxBreadcrumbs)

	eventContext := Context{"value": "event-before"}
	eventBreadcrumbData := map[string]any{"value": "event-before"}
	userData := map[string]string{"value": "event-before"}
	event := &Event{
		Contexts: map[string]Context{"event": eventContext},
		Breadcrumbs: []*Breadcrumb{{
			Message: "event",
			Data:    eventBreadcrumbData,
		}},
		User: User{ID: "event", Data: userData},
	}

	transport := &serializationSafeScopeMergeTransport{}
	client, err := NewClient(ClientOptions{
		Dsn:            "https://key@sentry.io/1",
		MaxBreadcrumbs: defaultMaxBreadcrumbs,
		Transport:      transport,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	require.NotNil(t, client.captureEvent(ctx, event))
	captured := requireSingleEvent(t, &transport.MockTransport)

	result := make(chan struct {
		payload []byte
		err     error
	}, 1)
	start := make(chan struct{})
	go func() {
		<-start
		var payload []byte
		var err error
		for range 1000 {
			payload, err = json.Marshal(captured)
			if err != nil {
				break
			}
		}
		result <- struct {
			payload []byte
			err     error
		}{payload: payload, err: err}
	}()

	close(start)
	for range 1000 {
		globalContext["value"] = "global-after"
		currentContext["value"] = "current-after"
		eventContext["value"] = "event-after"
		globalBreadcrumbData["value"] = "global-after"
		currentBreadcrumbData["value"] = "current-after"
		eventBreadcrumbData["value"] = "event-after"
		userData["value"] = "event-after"
	}

	serialized := <-result
	require.NoError(t, serialized.err)
	var got struct {
		Contexts    map[string]Context `json:"contexts"`
		Breadcrumbs []*Breadcrumb      `json:"breadcrumbs"`
		User        User               `json:"user"`
	}
	require.NoError(t, json.Unmarshal(serialized.payload, &got))
	require.Equal(t, "global-before", got.Contexts["global"]["value"])
	require.Equal(t, "current-before", got.Contexts["current"]["value"])
	require.Equal(t, "event-before", got.Contexts["event"]["value"])
	require.Equal(t, []string{"global", "current", "event"}, breadcrumbMessages(got.Breadcrumbs))
	require.Equal(t, "global-before", got.Breadcrumbs[0].Data["value"])
	require.Equal(t, "current-before", got.Breadcrumbs[1].Data["value"])
	require.Equal(t, "event-before", got.Breadcrumbs[2].Data["value"])
	require.Equal(t, "event-before", got.User.Data["value"])
}

type serializationSafeScopeMergeTransport struct {
	MockTransport
}

func (transport *serializationSafeScopeMergeTransport) SendEvent(event *Event) {
	event.MakeSerializationSafe()
	transport.MockTransport.SendEvent(event)
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

func newScopeMergeTestClient(t *testing.T, maxBreadcrumbs int) (*Client, *MockTransport) {
	t.Helper()
	transport := &MockTransport{}
	client, err := NewClient(ClientOptions{
		Dsn:            "https://key@sentry.io/1",
		MaxBreadcrumbs: maxBreadcrumbs,
		Transport:      transport,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client, transport
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

func attachmentNames(attachments []*Attachment) []string {
	names := make([]string, len(attachments))
	for i, attachment := range attachments {
		names[i] = attachment.Filename
	}
	return names
}
