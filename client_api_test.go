package sentry

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/report"
)

func TestNoopClient(t *testing.T) {
	t.Parallel()

	client := NewNoopClient()
	if client.IsEnabled() {
		t.Fatal("no-op client must be disabled")
	}

	options := client.Options()
	if !options.DisableLogs {
		t.Error("no-op client must disable logs")
	}
	if !options.DisableMetrics {
		t.Error("no-op client must disable metrics")
	}

	client.AddEventProcessor(func(event *Event, _ *EventHint) *Event { return event })
	client.SetExternalContextTraceResolver(func(context.Context) (TraceID, SpanID, bool) {
		return TraceID{}, SpanID{}, true
	})
	client.SetSDKIdentifier("custom")

	if got := client.CaptureMessage(context.Background(), "message", withLegacyScope(nil)); got != nil {
		t.Errorf("CaptureMessage returned %v, want nil", got)
	}
	if got := client.CaptureException(context.Background(), errors.New("boom"), withLegacyScope(nil)); got != nil {
		t.Errorf("CaptureException returned %v, want nil", got)
	}
	if got := client.CaptureCheckIn(t.Context(), &CheckIn{}, nil); got != nil {
		t.Errorf("CaptureCheckIn returned %v, want nil", got)
	}
	if got := client.CaptureEvent(context.Background(), NewEvent(), withLegacyScope(nil)); got != nil {
		t.Errorf("CaptureEvent returned %v, want nil", got)
	}
	if got := client.Recover(t.Context(), errors.New("boom")); got != nil {
		t.Errorf("Recover returned %v, want nil", got)
	}
	if got := client.Recover(t.Context(), errors.New("boom")); got != nil {
		t.Errorf("RecoverWithContext returned %v, want nil", got)
	}

	if got := client.EventFromMessage("message", LevelInfo); got != nil {
		t.Errorf("EventFromMessage returned %v, want nil", got)
	}
	if got := client.EventFromException(errors.New("boom"), LevelError); got != nil {
		t.Errorf("EventFromException returned %v, want nil", got)
	}
	if got := client.EventFromCheckIn(&CheckIn{}, nil); got != nil {
		t.Errorf("EventFromCheckIn returned %v, want nil", got)
	}

	if !client.Flush(time.Millisecond) {
		t.Error("no-op Flush must report success")
	}
	if !client.FlushWithContext(t.Context()) {
		t.Error("no-op FlushWithContext must report success")
	}
	client.Close()

	if got := client.GetDataCollection(); !reflect.DeepEqual(got, DataCollection{}) {
		t.Errorf("GetDataCollection returned %#v, want empty configuration", got)
	}
	if got := client.GetSDKIdentifier(); got != sdkIdentifier {
		t.Errorf("GetSDKIdentifier returned %q, want %q", got, sdkIdentifier)
	}
	if got := client.GetSDKVersion(); got != SDKVersion {
		t.Errorf("GetSDKVersion returned %q, want %q", got, SDKVersion)
	}
	if dsn := client.getDsn(); dsn != nil {
		t.Errorf("getDsn returned %#v, want nil", dsn)
	}
	if traceID, spanID, ok := client.externalTraceContextFromContext(t.Context()); ok || traceID != (TraceID{}) || spanID != (SpanID{}) {
		t.Errorf("external trace resolver returned (%v, %v, %v), want zero values", traceID, spanID, ok)
	}
	if client.captureLog(&Log{}, NewScope()) {
		t.Error("no-op client must discard logs")
	}
	if client.captureMetric(&Metric{}, NewScope()) {
		t.Error("no-op client must discard metrics")
	}
	client.recordDiscard(report.ReasonSampleRate, ratelimit.CategoryTransaction, 1)

	if event := NewScope().ApplyToEvent(NewEvent(), nil, nil); event == nil {
		t.Error("scope application with a nil client must normalize to the no-op client")
	}
	if got := DynamicSamplingContextFromScope(NewScope(), nil); got.HasEntries() || got.IsFrozen() {
		t.Errorf("DynamicSamplingContextFromScope returned %#v with nil client, want empty context", got)
	}
}

func TestNewClientReturnsEnabledClient(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientOptions{Transport: &MockTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	if !client.IsEnabled() {
		t.Error("NewClient returned a disabled client")
	}
}

// stubClient is a user-defined Client implementation used to exercise
// normalizeClient with typed-nil pointers of types other than *defaultClient.
type stubClient struct{ Client }

func TestNormalizeClient(t *testing.T) {
	t.Parallel()

	tests := map[string]Client{
		"nil interface":      nil,
		"nil default client": (*defaultClient)(nil),
		"nil custom client":  (*stubClient)(nil),
		"no-op client":       noopClient{},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if got := normalizeClient(input); got == nil || got.IsEnabled() {
				t.Fatalf("normalizeClient(%T) = %#v, want disabled non-nil client", input, got)
			}
		})
	}
}

func TestHubWithoutClientFlushSucceeds(t *testing.T) {
	t.Parallel()

	hub := NewHub(nil, NewScope())
	if !hub.Flush(time.Millisecond) {
		t.Error("Flush on a hub without a client must report success")
	}
	if !hub.FlushWithContext(t.Context()) {
		t.Error("FlushWithContext on a hub without a client must report success")
	}
}
