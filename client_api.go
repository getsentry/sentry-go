package sentry

import (
	"context"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/report"
)

// Client captures and delivers telemetry to Sentry.
type Client interface {
	IsEnabled() bool
	AddEventProcessor(EventProcessor)
	SetExternalContextTraceResolver(func(context.Context) (TraceID, SpanID, bool))
	Options() ClientOptions
	clientOptions() *ClientOptions
	GetDataCollection() DataCollection
	CaptureMessage(string, *EventHint, EventModifier) *EventID
	CaptureException(error, *EventHint, EventModifier) *EventID
	CaptureCheckIn(*CheckIn, *MonitorConfig, EventModifier) *EventID
	CaptureEvent(*Event, *EventHint, EventModifier) *EventID
	Recover(any, *EventHint, EventModifier) *EventID
	RecoverWithContext(context.Context, any, *EventHint, EventModifier) *EventID
	Flush(time.Duration) bool
	FlushWithContext(context.Context) bool
	Close()
	EventFromMessage(string, Level) *Event
	EventFromException(error, Level) *Event
	EventFromCheckIn(*CheckIn, *MonitorConfig) *Event
	SetSDKIdentifier(string)
	GetSDKIdentifier() string
	GetSDKVersion() string
	externalTraceContextFromContext(context.Context) (TraceID, SpanID, bool)
	captureLog(*Log, *Scope) bool
	captureMetric(*Metric, *Scope) bool
	recordDiscard(report.DiscardReason, ratelimit.Category, int64)
	getDsn() *protocol.Dsn
}

type noopClient struct{}

// noopClientOptions is shared by every noopClient via clientOptions. It must
// never be mutated: callers of clientOptions must treat the returned options
// as read-only.
var noopClientOptions = ClientOptions{
	DisableLogs:    true,
	DisableMetrics: true,
}

// NewNoopClient returns a Client that discards all telemetry.
func NewNoopClient() Client { return noopClient{} }

func normalizeClient(value Client) Client {
	if value == nil {
		return noopClient{}
	}
	// Guard against typed-nil Client implementations (e.g. a nil
	// *defaultClient or a nil pointer to a user-defined implementation),
	// which would otherwise panic on first use.
	if v := reflect.ValueOf(value); v.Kind() == reflect.Pointer && v.IsNil() {
		return noopClient{}
	}
	return value
}

func (noopClient) IsEnabled() bool                  { return false }
func (noopClient) AddEventProcessor(EventProcessor) {}
func (noopClient) SetExternalContextTraceResolver(func(context.Context) (TraceID, SpanID, bool)) {
}
func (noopClient) Options() ClientOptions                                     { return noopClientOptions }
func (noopClient) clientOptions() *ClientOptions                              { return &noopClientOptions }
func (noopClient) GetDataCollection() DataCollection                          { return DataCollection{} }
func (noopClient) CaptureMessage(string, *EventHint, EventModifier) *EventID  { return nil }
func (noopClient) CaptureException(error, *EventHint, EventModifier) *EventID { return nil }
func (noopClient) CaptureCheckIn(*CheckIn, *MonitorConfig, EventModifier) *EventID {
	return nil
}
func (noopClient) CaptureEvent(*Event, *EventHint, EventModifier) *EventID { return nil }
func (noopClient) Recover(any, *EventHint, EventModifier) *EventID         { return nil }
func (noopClient) RecoverWithContext(context.Context, any, *EventHint, EventModifier) *EventID {
	return nil
}
func (noopClient) Flush(time.Duration) bool                         { return true }
func (noopClient) FlushWithContext(context.Context) bool            { return true }
func (noopClient) Close()                                           {}
func (noopClient) EventFromMessage(string, Level) *Event            { return nil }
func (noopClient) EventFromException(error, Level) *Event           { return nil }
func (noopClient) EventFromCheckIn(*CheckIn, *MonitorConfig) *Event { return nil }
func (noopClient) SetSDKIdentifier(string)                          {}
func (noopClient) GetSDKIdentifier() string                         { return sdkIdentifier }
func (noopClient) GetSDKVersion() string                            { return SDKVersion }
func (noopClient) externalTraceContextFromContext(context.Context) (TraceID, SpanID, bool) {
	return TraceID{}, SpanID{}, false
}
func (noopClient) captureLog(*Log, *Scope) bool                                  { return false }
func (noopClient) captureMetric(*Metric, *Scope) bool                            { return false }
func (noopClient) recordDiscard(report.DiscardReason, ratelimit.Category, int64) {}
func (noopClient) getDsn() *protocol.Dsn                                         { return nil }
