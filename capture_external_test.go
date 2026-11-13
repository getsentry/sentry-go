package sentry_test

import (
	"encoding/json"
	"errors"
	"flag"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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
		scope.AddBreadcrumb(&sentry.Breadcrumb{Category: "auth", Message: "logged in", Data: map[string]any{"method": "sso"}}, 10)
		scope.AddAttachment(&sentry.Attachment{Filename: "log.txt", ContentType: "text/plain", Payload: []byte("attached")})
		request := httptest.NewRequest("GET", "http://example.com/orders/7?page=2", nil)
		request.Header.Set("User-Agent", "golden")
		scope.SetRequest(request)

		root := sentry.StartTransaction(ctx, "GET /orders/:id",
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(sentry.SourceRoute),
			sentry.WithSpanOrigin(sentry.SpanOriginStdLib),
		)
		root.SetTag("route", "orders")
		root.SetData("http.response.status_code", 200)
		root.Status = sentry.SpanStatusOK
		child := root.StartChild("db.query", sentry.WithDescription("SELECT 1"))
		child.SetTag("db", "primary")
		child.SetData("db.system", "postgresql")
		child.Status = sentry.SpanStatusOK
		child.Finish()
		event := sentry.NewEvent()
		event.Level = sentry.LevelError
		event.Message = "checkout failed"
		event.Transaction = "GET /orders/:id"
		event.SetException(errors.New("boom"), 1)
		sentry.CaptureEvent(root.Context(), event)
		root.Finish()

		sentry.CaptureCheckIn(ctx,
			&sentry.CheckIn{MonitorSlug: "nightly", Status: sentry.CheckInStatusOK, Duration: 2 * time.Second},
			&sentry.MonitorConfig{Schedule: sentry.CrontabSchedule("0 3 * * *"), CheckInMargin: 5, MaxRuntime: 30, Timezone: "UTC", FailureIssueThreshold: 2, RecoveryThreshold: 1},
		)
		sentry.CaptureCheckIn(ctx, &sentry.CheckIn{MonitorSlug: "nightly", Status: sentry.CheckInStatusInProgress}, nil)
		sentry.NewLogger(ctx).Info().String("key", "value").Emitf("hello %s", "world")
		sentry.NewMeter(ctx).Count("orders", 1, sentry.WithUnit("order"), sentry.WithAttributes(attribute.String("key", "value")))

		f.Flush()
		for _, envelope := range f.Envelopes() {
			kind := string(envelope.Items[0].Header.Type)
			if _, seen := got[kind]; seen {
				kind += "_bare"
			}
			got[kind] = envelopeJSON(envelope)
		}
	}, sentrytest.WithClientOptions(sentry.ClientOptions{
		Release:          "app@1.0.0",
		Environment:      "test",
		ServerName:       "golden",
		EnableTracing:    true,
		TracesSampleRate: 1,
	}))

	ignored := cmp.Options{
		ignoreTimestamps(),
		ignoreIDs(),
		ignoreValues("length", "version", "sentry.sdk.version", "frames", "modules", "device", "os", "runtime"),
	}
	if strings.Contains(string(got["check_in_bare"]), "monitor_config") {
		t.Errorf("check-in without a monitor config has one:\n%s", got["check_in_bare"])
	}
	for _, kind := range []string{"event", "transaction", "check_in", "log", "trace_metric"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join("testdata", "envelope", kind+".json")
			if flag.Lookup("update").Value.String() == "true" {
				sentry.WriteGoldenFile(t, path, got[kind])
			}
			var want, have any
			if err := json.Unmarshal([]byte(sentry.ReadOrGenerateGoldenFile(t, path, got[kind])), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(got[kind], &have); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, have, ignored); diff != "" {
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
	encoded, err := json.MarshalIndent(map[string]any{"header": envelope.Header, "items": items}, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(encoded, '\n')
}

// ignoreValues ignores the values of the given JSON keys. The keys still have
// to be present with a value of the same JSON type.
func ignoreValues(keys ...string) cmp.Option {
	return cmp.FilterPath(func(path cmp.Path) bool {
		index, ok := path.Last().(cmp.MapIndex)
		return ok && slices.Contains(keys, index.Key().String())
	}, cmp.Comparer(func(x, y any) bool { return reflect.TypeOf(x) == reflect.TypeOf(y) }))
}

func ignoreTimestamps() cmp.Option {
	return ignoreValues("timestamp", "start_timestamp", "sent_at")
}

func ignoreIDs() cmp.Option {
	return ignoreValues("event_id", "trace_id", "span_id", "parent_span_id", "check_in_id")
}
