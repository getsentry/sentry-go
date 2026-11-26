package sentry_test

import (
	"encoding/json"
	"errors"
	"flag"
	"math"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/getsentry/sentry-go/protocol"
)

func TestEnvelopes(t *testing.T) {
	t.Parallel()
	got := map[string][]byte{}
	sentrytest.Run(t, func(_ *testing.T, f *sentrytest.Fixture) {
		ctx, scope := f.Context, f.Scope
		scope.SetTag("tag", "value")
		scope.SetUser(sentry.User{ID: "42", Data: map[string]string{"plan": "pro"}})
		scope.SetFingerprint([]string{"fingerprint"})
		scope.SetContext("custom", sentry.Context{"key": "value", "nested": map[string]any{"n": 1}})
		scope.AddBreadcrumb(&sentry.Breadcrumb{Category: "auth", Message: "logged in", Data: map[string]any{"method": "sso"}})
		scope.AddAttachment(&sentry.Attachment{Filename: "log.txt", ContentType: "text/plain", Payload: []byte("attached")})
		request := httptest.NewRequest("GET", "http://example.com/orders/7?page=2", nil)
		request.Header.Set("User-Agent", "golden")
		scope.SetRequest(request)

		rootCtx, root := sentry.StartSpan(ctx, "GET /orders/:id",
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(sentry.SourceRoute),
			sentry.WithSpanOrigin(sentry.SpanOriginStdLib),
		)
		root.SetTag("route", "orders")
		root.SetData("http.response.status_code", 200)
		root.Status = sentry.SpanStatusOK
		_, child := sentry.StartSpan(rootCtx, "SELECT 1", sentry.WithOpName("db.query"))
		child.SetTag("db", "primary")
		child.SetData("db.system", "postgresql")
		child.Status = sentry.SpanStatusOK
		child.Finish()
		event := sentry.NewEvent()
		event.Level = sentry.LevelError
		event.Message = "checkout failed"
		event.Transaction = "GET /orders/:id"
		event.SetException(errors.New("boom"), 1)
		event.Exception[0].Stacktrace = &sentry.Stacktrace{Frames: []sentry.Frame{{Function: "checkout", Module: "shop", Filename: "shop.go", Lineno: 7, InApp: true}}}
		sentry.CaptureEvent(rootCtx, event)
		root.Finish()

		sentry.CaptureCheckIn(ctx,
			&sentry.CheckIn{MonitorSlug: "nightly", Status: sentry.CheckInStatusOK, Duration: 2 * time.Second},
			&sentry.MonitorConfig{Schedule: sentry.CrontabSchedule("0 3 * * *"), CheckInMargin: 5, MaxRuntime: 30, Timezone: "UTC", FailureIssueThreshold: 2, RecoveryThreshold: 1},
		)
		sentry.NewLogger(ctx).Info().String("key", "value").Emitf("hello %s", "world")
		sentry.NewMeter(ctx).Count("orders", 1, sentry.WithUnit("order"), sentry.WithAttributes(attribute.String("key", "value")))

		f.Flush()
		for _, envelope := range f.Envelopes() {
			got[string(envelope.Items[0].Header.Type)] = envelopeJSON(envelope)
		}
	}, sentrytest.WithClientOptions(sentry.ClientOptions{
		Release:          "app@1.0.0",
		Environment:      "test",
		ServerName:       "golden",
		TracesSampleRate: new(1.0),
		// The device, runtime and module lists differ per machine.
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			return slices.DeleteFunc(integrations, func(integration sentry.Integration) bool {
				return integration.Name() == "Environment" || integration.Name() == "Modules"
			})
		},
	}))

	for _, kind := range []string{"event", "transaction", "check_in", "log", "trace_metric"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join("testdata", "envelope", kind+".json")
			if flag.Lookup("update").Value.String() == "true" {
				sentry.WriteGoldenFile(t, path, got[kind])
			}
			want := sentry.ReadOrGenerateGoldenFile(t, path, got[kind])
			if diff := cmp.Diff(want, string(got[kind])); diff != "" {
				t.Errorf("envelope mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func envelopeJSON(envelope *protocol.Envelope) []byte {
	items := make([]map[string]any, len(envelope.Items))
	for i, item := range envelope.Items {
		var payload any = string(item.Payload)
		if item.Header.Type != protocol.EnvelopeItemTypeAttachment {
			payload = json.RawMessage(item.Payload)
		}
		items[i] = map[string]any{"header": item.Header, "payload": payload}
	}
	encoded, err := json.Marshal(map[string]any{"header": envelope.Header, "items": items})
	if err != nil {
		panic(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		panic(err)
	}
	dropVolatile(decoded)
	encoded, err = json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(encoded, '\n')
}

// volatile lists the JSON keys whose values differ between runs, machines or
// releases. Goldens leave them out.
var volatile = []string{
	"timestamp", "start_timestamp", "sent_at",
	"event_id", "trace_id", "span_id", "parent_span_id", "check_in_id",
	"length", "version", "sentry.sdk.version",
}

func dropVolatile(value any) {
	switch value := value.(type) {
	case map[string]any:
		for _, key := range volatile {
			delete(value, key)
		}
		for _, child := range value {
			dropVolatile(child)
		}
	case []any:
		for _, child := range value {
			dropVolatile(child)
		}
	}
}

type panicsOnMarshal struct{}

func (panicsOnMarshal) MarshalJSON() ([]byte, error) { panic("marshal") }

func TestCaptureWithUnencodableValue(t *testing.T) {
	t.Parallel()
	loop := map[string]any{}
	loop["loop"] = loop
	tests := []struct {
		name  string
		value any
	}{
		{"not a number", math.NaN()},
		{"marshaler that panics", panicsOnMarshal{}},
		{"map that contains itself", loop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				f.Scope.SetContext("custom", sentry.Context{"bad": tt.value, "good": struct{ Value string }{"kept"}})
				sentry.CaptureMessage(f.Context, "message")
				f.Flush()
				envelopes := f.Envelopes()
				if len(envelopes) != 1 {
					t.Fatalf("got %d envelopes, want 1", len(envelopes))
				}
				payload := string(envelopes[0].Items[0].Payload)
				if !strings.Contains(payload, `"good":{"Value":"kept"}`) || strings.Contains(payload, `"bad"`) {
					t.Errorf("payload = %s", payload)
				}
			})
		})
	}
}

func TestCaptureAddsModules(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		sentry.CaptureMessage(f.Context, "message")
		f.Flush()
		if events := f.Events(); len(events) != 1 || len(events[0].Modules) == 0 {
			t.Errorf("sent events have no modules: %+v", events)
		}
	})
}
