package sentry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCaptureTestClient(t *testing.T, options ClientOptions) (*Client, *MockTransport) {
	t.Helper()
	transport := new(MockTransport)
	options.Transport = transport
	options.Integrations = func([]Integration) []Integration { return nil }
	client, err := NewClient(options)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client, transport
}

func TestCaptureUsesContextClientAndScope(t *testing.T) {
	globalClient, globalTransport := newCaptureTestClient(t, ClientOptions{})
	localClient, localTransport := newCaptureTestClient(t, ClientOptions{})

	previousClient := globalClientSnapshot()
	setGlobalClient(globalClient)
	t.Cleanup(func() { setGlobalClient(previousClient) })

	ctx, scope := WithIsolationScope(context.Background())
	ctx = ContextWithClient(ctx, localClient)
	scope.SetTag("source", "context")

	id := CaptureMessage(ctx, "package")
	require.NotNil(t, id)
	require.Len(t, localTransport.Events(), 1)
	assert.Equal(t, "context", localTransport.Events()[0].Tags["source"])
	assert.Empty(t, globalTransport.Events())

	require.NotNil(t, localClient.captureMessage(ctx, "client"))
	require.Len(t, localTransport.Events(), 2)

	disabledCtx := ContextWithClient(ctx, NewNoopClient())
	assert.Nil(t, CaptureMessage(disabledCtx, "suppressed"))
	require.Len(t, localTransport.Events(), 2)
}

func TestCaptureWithoutIsolationScopeDoesNotReuseGlobalTrace(t *testing.T) {
	client, transport := newCaptureTestClient(t, ClientOptions{})
	previousClient := globalClientSnapshot()
	setGlobalClient(client)
	t.Cleanup(func() { setGlobalClient(previousClient) })

	require.NotNil(t, CaptureMessage(context.Background(), "background"))
	require.Len(t, transport.Events(), 1)
	assert.NotContains(t, transport.Events()[0].Contexts, "trace")
}

func TestCaptureOptions(t *testing.T) {
	client, transport := newCaptureTestClient(t, ClientOptions{})
	scope := NewScope()
	scope.SetLevel(LevelWarning)
	ctx := ContextWithScope(context.Background(), scope)

	tests := []struct {
		name    string
		capture func()
		want    Level
	}{
		{name: "helper default below scope", capture: func() { client.captureException(ctx, errors.New("boom")) }, want: LevelWarning},
		{name: "scope above event", capture: func() { client.captureEvent(ctx, &Event{Level: LevelDebug}) }, want: LevelWarning},
		{name: "option above event", capture: func() { client.captureEvent(ctx, &Event{Level: LevelDebug}, WithLevel(LevelFatal)) }, want: LevelFatal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.capture()
			assert.Equal(t, test.want, transport.Events()[len(transport.Events())-1].Level)
		})
	}
}

func TestCaptureCopiesHintAndUsesContext(t *testing.T) {
	type key struct{}
	providedCtx := context.WithValue(context.Background(), key{}, "provided")
	captureCtx := context.WithValue(context.Background(), key{}, "capture")
	provided := &EventHint{Context: providedCtx, Data: "data"}

	var received *EventHint
	client, _ := newCaptureTestClient(t, ClientOptions{
		BeforeSend: func(event *Event, hint *EventHint) *Event {
			received = hint
			return event
		},
	})
	require.NotNil(t, client.captureMessage(captureCtx, "message", WithEventHint(provided)))

	require.NotNil(t, received)
	assert.NotSame(t, provided, received)
	assert.Equal(t, "data", received.Data)
	assert.Equal(t, "capture", received.Context.Value(key{}))
	assert.Same(t, providedCtx, provided.Context)
}

func TestCaptureLastEventID(t *testing.T) {
	client, _ := newCaptureTestClient(t, ClientOptions{})
	local := NewScope()
	ctx := ContextWithScope(context.Background(), local)
	local.setLastEventID("")

	id := client.captureMessage(ctx, "message")
	require.NotNil(t, id)
	assert.Equal(t, *id, LastEventID(ctx))

	client.captureCheckIn(ctx, &CheckIn{MonitorSlug: "cron", Status: CheckInStatusOK}, nil)
	assert.Equal(t, *id, LastEventID(ctx))
	client.captureEvent(ctx, &Event{Type: transactionType})
	assert.Equal(t, *id, LastEventID(ctx))

	global := GlobalScope()
	previousClient := globalClientSnapshot()
	previousID := global.lastEventIDSnapshot()
	setGlobalClient(client)
	t.Cleanup(func() {
		setGlobalClient(previousClient)
		global.setLastEventID(previousID)
	})
	globalID := CaptureMessage(context.Background(), "global")
	require.NotNil(t, globalID)
	assert.Equal(t, *globalID, LastEventID(context.Background()))
}

func TestDisabledCaptureDoesNotResolveOptions(t *testing.T) {
	called := false
	option := func(*captureOptions) { called = true }
	assert.Nil(t, NewNoopClient().captureMessage(context.Background(), "message", option))
	assert.False(t, called)
}
