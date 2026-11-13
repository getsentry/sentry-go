package sentry_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
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

func TestChangesAfterCaptureAreNotSent(t *testing.T) {
	t.Parallel()
	const changed = "CHANGED"
	var kept sync.Map
	options := sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1,
		BeforeSendLog: func(log *sentry.Log) *sentry.Log {
			kept.Store(log.Body, log)
			return log
		},
		BeforeSendMetric: func(metric *sentry.Metric) *sentry.Metric {
			kept.Store(metric.Name, metric)
			return metric
		},
	}
	tests := []struct {
		name    string
		capture func(ctx context.Context, scope *sentry.Scope) (change func())
	}{
		{"every event field", func(ctx context.Context, _ *sentry.Scope) func() {
			event := &sentry.Event{}
			fill(reflect.ValueOf(event).Elem())
			event.Type = ""
			sentry.CaptureEvent(ctx, event)
			return func() { scribble(reflect.ValueOf(event).Elem(), changed) }
		}},
		{"scope context", func(ctx context.Context, scope *sentry.Scope) func() {
			fields := sentry.Context{
				"key": "value", "nested": map[string]any{"key": "value"}, "list": []any{"value"},
				"strings": []string{"value"}, "labels": map[string]string{"key": "value"},
			}
			scope.SetContext("fields", fields)
			sentry.CaptureMessage(ctx, "message")
			return func() {
				fields["key"] = changed
				fields["nested"].(map[string]any)["key"] = changed
				fields["list"].([]any)[0] = changed
				fields["strings"].([]string)[0] = changed
				fields["labels"].(map[string]string)["key"] = changed
			}
		}},
		{"monitor config", func(ctx context.Context, _ *sentry.Scope) func() {
			config := &sentry.MonitorConfig{Schedule: sentry.CrontabSchedule("* * * * *"), Timezone: "UTC"}
			sentry.CaptureCheckIn(ctx, &sentry.CheckIn{MonitorSlug: "job", Status: sentry.CheckInStatusOK}, config)
			return func() { config.Timezone = changed }
		}},
		{"log kept by a hook", func(ctx context.Context, _ *sentry.Scope) func() {
			sentry.NewLogger(ctx).Info().String("key", "value").Emit("kept log")
			return func() {
				log, _ := kept.Load("kept log")
				log.(*sentry.Log).Body = changed
				log.(*sentry.Log).Attributes["key"] = attribute.StringValue(changed)
			}
		}},
		{"metric kept by a hook", func(ctx context.Context, _ *sentry.Scope) func() {
			sentry.NewMeter(ctx).Count("kept metric", 1, sentry.WithAttributes(attribute.String("key", "value")))
			return func() {
				metric, _ := kept.Load("kept metric")
				metric.(*sentry.Metric).Unit = changed
				metric.(*sentry.Metric).Attributes["key"] = attribute.StringValue(changed)
			}
		}},
		{"span data", func(ctx context.Context, _ *sentry.Scope) func() {
			root := sentry.StartTransaction(ctx, "transaction")
			child := root.StartChild("child")
			value := map[string]any{"key": "value"}
			child.SetData("value", value)
			child.Finish()
			root.Finish()
			return func() {
				value["key"] = changed
				child.SetData("late", changed)
				child.SetTag("late", changed)
				child.Description = changed
				root.SetData("late", changed)
			}
		}},
		{"span data that uses its span", func(ctx context.Context, _ *sentry.Scope) func() {
			root := sentry.StartTransaction(ctx, "transaction")
			child := root.StartChild("child")
			child.SetData("value", usesSpan{child})
			child.Finish()
			root.Finish()
			return func() {}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				change := tt.capture(f.Context, f.Scope)
				change()
				f.Flush()
				envelopes := f.Envelopes()
				if len(envelopes) == 0 {
					t.Fatal("nothing was sent")
				}
				for _, envelope := range envelopes {
					header, _ := json.Marshal(envelope.Header)
					if strings.Contains(string(header), changed) {
						t.Errorf("envelope header has a value written after capture returned:\n%s", header)
					}
					for _, item := range envelope.Items {
						header, _ := json.Marshal(item.Header)
						if strings.Contains(string(header)+string(item.Payload), changed) {
							t.Errorf("item has a value written after capture returned:\n%s\n%s", header, item.Payload)
						}
					}
				}
			}, sentrytest.WithHeldDelivery(), sentrytest.WithClientOptions(options))
		})
	}
}

// usesSpan is a span data value whose encoding calls back into its span.
type usesSpan struct{ span *sentry.Span }

func (v usesSpan) MarshalJSON() ([]byte, error) {
	v.span.SetTag("encoded", "true")
	return []byte(`"value"`), nil
}

// fill sets every exported field reachable from v.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("value")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case reflect.Map:
		value := reflect.New(v.Type().Elem()).Elem()
		fill(value)
		v.Set(reflect.MakeMap(v.Type()))
		v.SetMapIndex(reflect.ValueOf("key"), value)
	case reflect.Interface:
		v.Set(reflect.ValueOf(map[string]any{
			"key":     "value",
			"list":    []any{"value"},
			"strings": []string{"value"},
			"pointer": &struct{ Name string }{"value"},
		}))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(1, 0)))
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i))
			}
		}
	}
}

// scribble overwrites every string reachable from v with marker, in place.
func scribble(v reflect.Value, marker string) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(marker)
		}
	case reflect.Pointer:
		if !v.IsNil() {
			scribble(v.Elem(), marker)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Elem().Kind() == reflect.String && v.CanSet() {
			v.Set(reflect.ValueOf(marker))
			return
		}
		scribble(v.Elem(), marker)
	case reflect.Slice:
		for i := range v.Len() {
			scribble(v.Index(i), marker)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			value := v.MapIndex(key)
			if value.Kind() == reflect.Interface {
				value = value.Elem()
			}
			if value.Kind() == reflect.String {
				v.SetMapIndex(key, reflect.ValueOf(marker))
				continue
			}
			scribble(value, marker)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				scribble(v.Field(i), marker)
			}
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
				f.Scope.SetContext("custom", sentry.Context{"bad": tt.value, "good": "kept"})
				sentry.CaptureMessage(f.Context, "message")
				f.Flush()
				envelopes := f.Envelopes()
				if len(envelopes) != 1 {
					t.Fatalf("got %d envelopes, want 1", len(envelopes))
				}
				payload := string(envelopes[0].Items[0].Payload)
				if !strings.Contains(payload, `"good":"kept"`) || strings.Contains(payload, `"bad"`) {
					t.Errorf("payload = %s", payload)
				}
			})
		})
	}
}
