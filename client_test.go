package sentry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pkgErrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestNewClientAllowsEmptyDSN(t *testing.T) {
	transport := &MockTransport{}
	client, err := NewClient(ClientOptions{
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("expected no error when creating client without a DNS but got %v", err)
	}

	client.CaptureException(errors.New("custom error"), nil, &MockScope{})
	assertEqual(t, transport.lastEvent.Exception[0].Value, "custom error")
}

type customComplexError struct {
	Message string
}

func (e customComplexError) Error() string {
	return "customComplexError: " + e.Message
}

func (e customComplexError) AnswerToLife() string {
	return "42"
}

func setupClientTest() (*Client, *MockScope, *MockTransport) {
	scope := &MockScope{}
	transport := &MockTransport{}
	client, _ := NewClient(ClientOptions{
		Dsn:       "http://whatever@example.com/1337",
		Transport: transport,
		// keep default buffers enabled
		Integrations: func(_ []Integration) []Integration {
			return []Integration{}
		},
	})

	return client, scope, transport
}
func TestCaptureMessageShouldSendEventWithProvidedMessage(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureMessage("foo", nil, scope)
	assertEqual(t, transport.lastEvent.Message, "foo")
}

func TestCaptureMessageShouldSucceedWithoutNilScope(t *testing.T) {
	client, _, transport := setupClientTest()
	client.CaptureMessage("foo", nil, nil)
	assertEqual(t, transport.lastEvent.Message, "foo")
}

func TestCaptureMessageEmptyString(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureMessage("", nil, scope)
	want := &Event{
		Exception: []Exception{
			{
				Type:       "sentry.usageError",
				Value:      "CaptureMessage called with empty message",
				Stacktrace: &Stacktrace{Frames: []Frame{}},
			},
		},
	}
	got := transport.lastEvent
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{}, "sdkMetaData"),
		cmp.Transformer("SimplifiedEvent", func(e *Event) *Event {
			return &Event{
				Exception: e.Exception,
			}
		}),
	}

	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

type customErr struct{}

func (e *customErr) Error() string {
	return "wat"
}

type customErrWithCause struct{ cause error }

func (e *customErrWithCause) Error() string {
	return "err"
}

func (e *customErrWithCause) Cause() error {
	return e.cause
}

type wrappedError struct{ original error }

func (e wrappedError) Error() string {
	return "wrapped: " + e.original.Error()
}

func (e wrappedError) Unwrap() error {
	return e.original
}

type captureExceptionTestGroup struct {
	name  string
	tests []captureExceptionTest
}

type captureExceptionTest struct {
	name string
	err  error
	want []Exception
}

func TestCaptureException(t *testing.T) {
	basicTests := []captureExceptionTest{
		{
			name: "NilError",
			err:  nil,
			want: []Exception{
				{
					Type:       "sentry.usageError",
					Value:      "CaptureException called with nil error",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
				},
			},
		},
		{
			name: "SimpleError",
			err:  errors.New("custom error"),
			want: []Exception{
				{
					Type:       "*errors.errorString",
					Value:      "custom error",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
				},
			},
		},
	}

	errorChainTests := []captureExceptionTest{
		{
			name: "MostRecentErrorHasStack",
			err:  pkgErrors.WithStack(&customErr{}),
			want: []Exception{
				{
					Type:       "*sentry.customErr",
					Value:      "wat",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             MechanismTypeChained,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						Source:           MechanismTypeUnwrap,
						IsExceptionGroup: false,
					},
				},
				{
					Type:       "*errors.withStack",
					Value:      "wat",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             MechanismTypeGeneric,
						ExceptionID:      0,
						ParentID:         nil,
						Source:           "",
						IsExceptionGroup: false,
					},
				},
			},
		},
		{
			name: "ChainWithNilCause",
			err:  &customErrWithCause{cause: nil},
			want: []Exception{
				{
					Type:       "*sentry.customErrWithCause",
					Value:      "err",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
				},
			},
		},
		{
			name: "ChainWithoutStacktrace",
			err:  &customErrWithCause{cause: &customErr{}},
			want: []Exception{
				{
					Type:       "*sentry.customErr",
					Value:      "wat",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             MechanismTypeChained,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						Source:           "cause",
						IsExceptionGroup: false,
					},
				},
				{
					Type:       "*sentry.customErrWithCause",
					Value:      "err",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             MechanismTypeGeneric,
						ExceptionID:      0,
						ParentID:         nil,
						Source:           "",
						IsExceptionGroup: false,
					},
				},
			},
		},
		{
			name: "Go113Unwrap",
			err:  wrappedError{original: errors.New("original")},
			want: []Exception{
				{
					Type:       "*errors.errorString",
					Value:      "original",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             MechanismTypeChained,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						Source:           MechanismTypeUnwrap,
						IsExceptionGroup: false,
					},
				},
				{
					Type:       "sentry.wrappedError",
					Value:      "wrapped: original",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             MechanismTypeGeneric,
						ExceptionID:      0,
						ParentID:         nil,
						Source:           "",
						IsExceptionGroup: false,
					},
				},
			},
		},
	}

	tests := []captureExceptionTestGroup{
		{
			name:  "Basic",
			tests: basicTests,
		},
		{
			name:  "ErrorChain",
			tests: errorChainTests,
		},
	}

	for _, grp := range tests {
		for _, tt := range grp.tests {
			tt := tt
			t.Run(grp.name+"/"+tt.name, func(t *testing.T) {
				client, _, transport := setupClientTest()
				client.CaptureException(tt.err, nil, nil)
				if transport.lastEvent == nil {
					t.Fatal("missing event")
				}
				got := transport.lastEvent.Exception
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("Event mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestCaptureEvent(t *testing.T) {
	client, _, transport := setupClientTest()

	eventID := EventID("0123456789abcdef")
	timestamp := time.Now().UTC()
	serverName := "testServer"

	client.CaptureEvent(&Event{
		EventID:    eventID,
		Timestamp:  timestamp,
		ServerName: serverName,
	}, nil, nil)

	if transport.lastEvent == nil {
		t.Fatal("missing event")
	}
	want := &Event{
		EventID:    eventID,
		Timestamp:  timestamp,
		ServerName: serverName,
		Level:      LevelInfo,
		Platform:   "go",
		Sdk: SdkInfo{
			Name:         "sentry.go",
			Version:      SDKVersion,
			Integrations: []string{},
			Packages: []SdkPackage{
				{
					// FIXME: name format doesn't follow spec in
					// https://docs.sentry.io/development/sdk-dev/event-payloads/sdk/
					Name:    "sentry-go",
					Version: SDKVersion,
				},
				// TODO: perhaps the list of packages is incomplete or there
				// should not be any package at all. We may include references
				// to used integrations like http, echo, gin, etc.
			},
		},
	}
	got := transport.lastEvent
	opts := cmp.Options{cmpopts.IgnoreFields(Event{}, "Release", "sdkMetaData")}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestCaptureEventShouldSendEventWithMessage(t *testing.T) {
	client, scope, transport := setupClientTest()
	event := NewEvent()
	event.Message = "event message"
	client.CaptureEvent(event, nil, scope)
	assertEqual(t, transport.lastEvent.Message, "event message")
}

func TestCaptureEventNil(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureEvent(nil, nil, scope)
	want := &Event{
		Exception: []Exception{
			{
				Type:       "sentry.usageError",
				Value:      "CaptureEvent called with nil event",
				Stacktrace: &Stacktrace{Frames: []Frame{}},
			},
		},
	}
	got := transport.lastEvent
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{}, "sdkMetaData"),
		cmp.Transformer("SimplifiedEvent", func(e *Event) *Event {
			return &Event{
				Exception: e.Exception,
			}
		}),
	}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestCaptureCheckIn(t *testing.T) {
	tests := []struct {
		name           string
		checkIn        *CheckIn
		monitorConfig  *MonitorConfig
		expectNilEvent bool
	}{
		{
			name:           "Nil CheckIn",
			checkIn:        nil,
			monitorConfig:  nil,
			expectNilEvent: true,
		},
		{
			name: "Nil MonitorConfig",
			checkIn: &CheckIn{
				ID:          "66e1a05b182346f2aee5fd7f0dc9b44e",
				MonitorSlug: "cron",
				Status:      CheckInStatusOK,
				Duration:    time.Second * 10,
			},
			monitorConfig: nil,
		},
		{
			name: "IntervalSchedule",
			checkIn: &CheckIn{
				ID:          "66e1a05b182346f2aee5fd7f0dc9b44e",
				MonitorSlug: "cron",
				Status:      CheckInStatusInProgress,
				Duration:    time.Second * 10,
			},
			monitorConfig: &MonitorConfig{
				Schedule:              IntervalSchedule(1, MonitorScheduleUnitHour),
				CheckInMargin:         10,
				MaxRuntime:            5000,
				Timezone:              "Asia/Singapore",
				FailureIssueThreshold: 5,
				RecoveryThreshold:     10,
			},
		},
		{
			name: "CronSchedule",
			checkIn: &CheckIn{
				ID:          "66e1a05b182346f2aee5fd7f0dc9b44e",
				MonitorSlug: "cron",
				Status:      CheckInStatusInProgress,
				Duration:    time.Second * 10,
			},
			monitorConfig: &MonitorConfig{
				Schedule:              CrontabSchedule("40 * * * *"),
				CheckInMargin:         10,
				MaxRuntime:            5000,
				Timezone:              "Asia/Singapore",
				FailureIssueThreshold: 5,
				RecoveryThreshold:     10,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			client, _, transport := setupClientTest()
			client.CaptureCheckIn(tt.checkIn, tt.monitorConfig, nil)
			capturedEvent := transport.lastEvent

			if tt.expectNilEvent && capturedEvent == nil {
				// Event is nil as expected, nothing else to check
				return
			}

			if capturedEvent == nil {
				t.Fatal("missing event")
			}

			if capturedEvent.Type != checkInType {
				t.Errorf("Event type mismatch: want %s, got %s", checkInType, capturedEvent.Type)
			}

			if diff := cmp.Diff(capturedEvent.CheckIn, tt.checkIn); diff != "" {
				t.Errorf("CheckIn mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(capturedEvent.MonitorConfig, tt.monitorConfig); diff != "" {
				t.Errorf("CheckIn mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCaptureCheckInExistingID(t *testing.T) {
	client, _, _ := setupClientTest()

	monitorConfig := &MonitorConfig{
		Schedule:      IntervalSchedule(1, MonitorScheduleUnitDay),
		CheckInMargin: 30,
		MaxRuntime:    30,
		Timezone:      "UTC",
	}

	checkInID := client.CaptureCheckIn(&CheckIn{
		MonitorSlug: "cron",
		Status:      CheckInStatusInProgress,
		Duration:    time.Second,
	}, monitorConfig, nil)

	checkInID2 := client.CaptureCheckIn(&CheckIn{
		ID:          *checkInID,
		MonitorSlug: "cron",
		Status:      CheckInStatusOK,
		Duration:    time.Minute,
	}, monitorConfig, nil)

	if *checkInID != *checkInID2 {
		t.Errorf("Expecting equivalent CheckInID: %s and %s", *checkInID, *checkInID2)
	}
}

func TestSampleRateCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.SampleRate = 0.000000000000001

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestApplyToScopeCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	scope.shouldDropEvent = true

	client.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		if event == nil {
			t.Errorf("EventProcessor received nil Event")
		}
		return event
	})

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestBeforeSendCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.BeforeSend = func(_ *Event, _ *EventHint) *Event {
		return nil
	}

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestBeforeSendGetAccessToEventHint(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.BeforeSend = func(event *Event, hint *EventHint) *Event {
		if ex, ok := hint.OriginalException.(customComplexError); ok {
			event.Message = event.Exception[0].Value + " " + ex.AnswerToLife()
		}
		return event
	}
	ex := customComplexError{Message: "Foo"}

	client.CaptureException(ex, &EventHint{OriginalException: ex}, scope)

	assertEqual(t, transport.lastEvent.Message, "customComplexError: Foo 42")
}

func TestBeforeSendTransactionCanDropTransaction(t *testing.T) {
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
		BeforeSend: func(event *Event, _ *EventHint) *Event {
			t.Error("beforeSend should not be called")
			return event
		},
		BeforeSendTransaction: func(event *Event, _ *EventHint) *Event {
			assertEqual(t, event.Transaction, "Foo")
			return nil
		},
	})

	transaction := StartTransaction(ctx,
		"Foo",
	)
	transaction.Finish()

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestBeforeSendTransactionIsCalled(t *testing.T) {
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
		BeforeSend: func(event *Event, _ *EventHint) *Event {
			t.Error("beforeSend should not be called")
			return event
		},
		BeforeSendTransaction: func(event *Event, _ *EventHint) *Event {
			assertEqual(t, event.Transaction, "Foo")
			event.Transaction = "Bar"
			return event
		},
	})

	transaction := StartTransaction(ctx,
		"Foo",
	)
	transaction.Finish()

	lastEvent := transport.lastEvent
	assertEqual(t, lastEvent.Transaction, "Bar")
	// Make sure it's the same span
	assertEqual(t, lastEvent.Contexts["trace"]["span_id"], transaction.SpanID)
}

func TestIgnoreErrors(t *testing.T) {
	tests := map[string]struct {
		ignoreErrors []string
		message      string
		expectDrop   bool
	}{
		"No Match": {
			message:      "Foo",
			ignoreErrors: []string{"Bar", "Baz"},
			expectDrop:   false,
		},
		"Partial Match": {
			message:      "FooBar",
			ignoreErrors: []string{"Foo", "Baz"},
			expectDrop:   true,
		},
		"Exact Match": {
			message:      "Foo Bar",
			ignoreErrors: []string{"\\bFoo\\b", "Baz"},
			expectDrop:   true,
		},
		"Wildcard Match": {
			message:      "Foo",
			ignoreErrors: []string{"F*", "Bar"},
			expectDrop:   true,
		},
		"Match string but not pattern": {
			message:      "(Foo)",
			ignoreErrors: []string{"(Foo)"},
			expectDrop:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scope := &MockScope{}
			transport := &MockTransport{}
			client, err := NewClient(ClientOptions{
				Transport:    transport,
				IgnoreErrors: tt.ignoreErrors,
			})
			if err != nil {
				t.Fatal(err)
			}

			client.CaptureMessage(tt.message, nil, scope)

			dropped := transport.lastEvent == nil
			if tt.expectDrop != dropped {
				t.Errorf("expected event to be dropped")
			}
		})
	}
}

func TestIgnoreTransactions(t *testing.T) {
	tests := map[string]struct {
		ignoreTransactions []string
		transaction        string
		expectDrop         bool
	}{
		"No Match": {
			transaction:        "Foo",
			ignoreTransactions: []string{"Bar", "Baz"},
			expectDrop:         false,
		},
		"Partial Match": {
			transaction:        "FooBar",
			ignoreTransactions: []string{"Foo", "Baz"},
			expectDrop:         true,
		},
		"Exact Match": {
			transaction:        "Foo Bar",
			ignoreTransactions: []string{"\\bFoo\\b", "Baz"},
			expectDrop:         true,
		},
		"Wildcard Match": {
			transaction:        "Foo",
			ignoreTransactions: []string{"F*", "Bar"},
			expectDrop:         true,
		},
		"Match string but not pattern": {
			transaction:        "(Foo)",
			ignoreTransactions: []string{"(Foo)"},
			expectDrop:         true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &MockTransport{}
			ctx := NewTestContext(ClientOptions{
				EnableTracing:      true,
				TracesSampleRate:   1.0,
				Transport:          transport,
				IgnoreTransactions: tt.ignoreTransactions,
			})

			transaction := StartTransaction(ctx,
				tt.transaction,
			)
			transaction.Finish()

			dropped := transport.lastEvent == nil
			if tt.expectDrop != dropped {
				t.Errorf("expected event to be dropped")
			}
		})
	}
}

func TestTraceIgnoreStatusCode_EmptyCode(t *testing.T) {
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
	})

	transaction := StartTransaction(ctx, "test")
	// Transaction has no http.response.status_code
	transaction.Finish()

	dropped := transport.lastEvent == nil
	assertEqual(t, dropped, false, "expected transaction to not be dropped")
}

func TestTraceIgnoreStatusCodes(t *testing.T) {
	tests := map[string]struct {
		ignoreStatusCodes [][]int
		statusCode        interface{}
		expectDrop        bool
	}{
		"Default behavior: ignoreStatusCodes = nil, should drop 404s": {
			statusCode:        404,
			ignoreStatusCodes: nil,
			expectDrop:        true,
		},
		"Specify No ignored codes": {
			statusCode:        404,
			ignoreStatusCodes: [][]int{},
			expectDrop:        false,
		},
		"Status code not in ignore ranges": {
			statusCode:        500,
			ignoreStatusCodes: [][]int{{400, 405}},
			expectDrop:        false,
		},
		"404 in ignore range": {
			statusCode:        404,
			ignoreStatusCodes: [][]int{{400, 405}},
			expectDrop:        true,
		},
		"403 in ignore range": {
			statusCode:        403,
			ignoreStatusCodes: [][]int{{400, 405}},
			expectDrop:        true,
		},
		"200 not ignored": {
			statusCode:        200,
			ignoreStatusCodes: [][]int{{400, 405}},
			expectDrop:        false,
		},
		"wrong code not ignored": {
			statusCode:        "something",
			ignoreStatusCodes: [][]int{{400, 405}},
			expectDrop:        false,
		},
		"Single status code as single-element slice": {
			statusCode:        404,
			ignoreStatusCodes: [][]int{{404}},
			expectDrop:        true,
		},
		"Single status code not in single-element slice": {
			statusCode:        500,
			ignoreStatusCodes: [][]int{{404}},
			expectDrop:        false,
		},
		"Multiple single codes": {
			statusCode:        500,
			ignoreStatusCodes: [][]int{{404}, {500}},
			expectDrop:        true,
		},
		"Multiple ranges - code in first range": {
			statusCode:        404,
			ignoreStatusCodes: [][]int{{400, 405}, {500, 599}},
			expectDrop:        true,
		},
		"Multiple ranges - code in second range": {
			statusCode:        500,
			ignoreStatusCodes: [][]int{{400, 405}, {500, 599}},
			expectDrop:        true,
		},
		"Multiple ranges - code not in any range": {
			statusCode:        200,
			ignoreStatusCodes: [][]int{{400, 405}, {500, 599}},
			expectDrop:        false,
		},
		"Mixed single codes and ranges": {
			statusCode:        404,
			ignoreStatusCodes: [][]int{{404}, {500, 599}},
			expectDrop:        true,
		},
		"Mixed single codes and ranges - code in range": {
			statusCode:        500,
			ignoreStatusCodes: [][]int{{404}, {500, 599}},
			expectDrop:        true,
		},
		"Mixed single codes and ranges - code not matched": {
			statusCode:        200,
			ignoreStatusCodes: [][]int{{404}, {500, 599}},
			expectDrop:        false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &MockTransport{}
			ctx := NewTestContext(ClientOptions{
				EnableTracing:          true,
				TracesSampleRate:       1.0,
				Transport:              transport,
				TraceIgnoreStatusCodes: tt.ignoreStatusCodes,
			})

			transaction := StartTransaction(ctx, "test")
			// Simulate HTTP response data like the integrations do
			transaction.SetData("http.response.status_code", tt.statusCode)
			transaction.Finish()

			dropped := transport.lastEvent == nil
			if tt.expectDrop != dropped {
				if tt.expectDrop {
					t.Errorf("expected transaction with status code %d to be dropped", tt.statusCode)
				} else {
					t.Errorf("expected transaction with status code %d not to be dropped", tt.statusCode)
				}
			}
		})
	}
}

func TestSampleRate(t *testing.T) {
	tests := []struct {
		SampleRate float64
		// tolerated range is [SampleRate-MaxDelta, SampleRate+MaxDelta]
		MaxDelta float64
	}{
		{0.00, 0.0},
		{0.25, 0.2},
		{0.50, 0.2},
		{0.75, 0.2},
		{1.00, 0.0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprint(tt.SampleRate), func(t *testing.T) {
			var (
				total   uint64
				sampled uint64
			)
			// Call sample from multiple goroutines just like multiple hubs
			// sharing a client would. This should help uncover data races.
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 10000; j++ {
						atomic.AddUint64(&total, 1)
						s := sample(tt.SampleRate)
						switch tt.SampleRate {
						case 0:
							if s {
								panic("sampled true when rate is 0")
							}
						case 1:
							if !s {
								panic("sampled false when rate is 1")
							}
						}
						if s {
							atomic.AddUint64(&sampled, 1)
						}
					}
				}()
			}
			wg.Wait()

			rate := float64(sampled) / float64(total)
			if !(tt.SampleRate-tt.MaxDelta <= rate && rate <= tt.SampleRate+tt.MaxDelta) {
				t.Errorf("effective sample rate was %f, want %f±%f", rate, tt.SampleRate, tt.MaxDelta)
			}
		})
	}
}

func BenchmarkProcessEvent(b *testing.B) {
	c, err := NewClient(ClientOptions{
		SampleRate: 0.25,
		Transport:  &MockTransport{},
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		c.processEvent(&Event{}, nil, nil)
	}
}

func TestRecover(t *testing.T) {
	tests := []struct {
		v    interface{} // for panic(v)
		want *Event
	}{
		{
			errors.New("panic error"),
			&Event{
				Exception: []Exception{
					{
						Type:       "*errors.errorString",
						Value:      "panic error",
						Stacktrace: &Stacktrace{Frames: []Frame{}},
					},
				},
			},
		},
		{"panic string", &Event{Message: "panic string"}},
		// Arbitrary types should be converted to string:
		{101010, &Event{Message: "101010"}},
		{[]string{"", "", "hello"}, &Event{Message: `[]string{"", "", "hello"}`}},
		{&struct{ Field string }{"test"}, &Event{Message: `&struct { Field string }{Field:"test"}`}},
	}
	checkEvent := func(t *testing.T, events []*Event, want *Event) {
		t.Helper()
		if len(events) != 1 {
			b, err := json.MarshalIndent(events, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("events = %s\ngot %d events, want 1", b, len(events))
		}
		got := events[0]
		opts := cmp.Options{
			cmpopts.IgnoreFields(Event{}, "sdkMetaData"),
			cmp.Transformer("SimplifiedEvent", func(e *Event) *Event {
				return &Event{
					Message:   e.Message,
					Exception: e.Exception,
					Level:     e.Level,
				}
			}),
		}

		if diff := cmp.Diff(want, got, opts); diff != "" {
			t.Errorf("(-want +got):\n%s", diff)
		}
	}
	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("Recover/%v", tt.v), func(t *testing.T) {
			client, scope, transport := setupClientTest()
			func() {
				defer client.Recover(nil, nil, scope)
				panic(tt.v)
			}()
			tt.want.Level = LevelFatal
			checkEvent(t, transport.Events(), tt.want)
		})
		t.Run(fmt.Sprintf("RecoverWithContext/%v", tt.v), func(t *testing.T) {
			client, scope, transport := setupClientTest()
			var called bool
			client.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
				called = true
				if hint.Context != context.TODO() {
					t.Fatal("unexpected context value")
				}
				return event
			})
			func() {
				defer client.RecoverWithContext(context.TODO(), nil, nil, scope)
				panic(tt.v)
			}()
			tt.want.Level = LevelFatal
			checkEvent(t, transport.Events(), tt.want)
			if !called {
				t.Error("event processor not called, could not test that hint has context")
			}
		})
	}
}

func TestCustomMaxSpansProperty(t *testing.T) {
	client, _, _ := setupClientTest()
	assertEqual(t, client.Options().MaxSpans, defaultMaxSpans)

	client.options.MaxSpans = 2000
	assertEqual(t, client.Options().MaxSpans, 2000)

	properClient, _ := NewClient(ClientOptions{
		MaxSpans:  3000,
		Transport: &MockTransport{},
	})

	assertEqual(t, properClient.Options().MaxSpans, 3000)
}

func TestSDKIdentifier(t *testing.T) {
	client, _, _ := setupClientTest()
	assertEqual(t, client.GetSDKIdentifier(), "sentry.go")

	client.SetSDKIdentifier("sentry.go.test")
	assertEqual(t, client.GetSDKIdentifier(), "sentry.go.test")
}

func TestClientSetsUpTransport(t *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn: testDsn,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, fmt.Errorf("mock transport - no real connections")
				},
			},
		},
		Transport: &MockTransport{},
	})
	require.IsType(t, &MockTransport{}, client.Transport)

	client, _ = NewClient(ClientOptions{})
	require.IsType(t, &noopTransport{}, client.Transport)
}

// func TestClient_SetupTelemetryBuffer_WithDSN(t *testing.T) {
//	client, err := NewClient(ClientOptions{
//		Dsn: "https://public@localhost/1",
//	})
//	if err != nil {
//		t.Fatalf("unexpected error: %v", err)
//	}
//
//	if client.telemetryBuffer == nil {
//		t.Fatal("expected telemetryBuffer to be initialized")
//	}
//
//	if _, ok := client.Transport.(*internalAsyncTransportAdapter); !ok {
//		t.Fatalf("expected internalAsyncTransportAdapter, got %T", client.Transport)
//	}
//
//	if !client.telemetryBuffer.Add(NewEvent()) {
//		t.Fatal("expected Add to succeed with default buffers")
//	}
//}

func TestClient_SetupTelemetryBuffer_NoDSN(t *testing.T) {
	var buf bytes.Buffer
	debuglog.SetOutput(&buf)
	defer debuglog.SetOutput(&bytes.Buffer{})

	client, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.telemetryBuffer != nil {
		t.Fatal("expected telemetryBuffer to be nil when DSN is missing")
	}

	if _, ok := client.Transport.(*noopTransport); !ok {
		t.Fatalf("expected noopTransport, got %T", client.Transport)
	}
}
