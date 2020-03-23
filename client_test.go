package sentry

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	pkgErrors "github.com/pkg/errors"
)

func TestNewClientAllowsEmptyDSN(t *testing.T) {
	transport := &TransportMock{}
	client, err := NewClient(ClientOptions{
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("expected no error when creating client without a DNS but got %v", err)
	}

	client.CaptureException(errors.New("custom error"), nil, &ScopeMock{})
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

func setupClientTest() (*Client, *ScopeMock, *TransportMock) {
	scope := &ScopeMock{}
	transport := &TransportMock{}
	client, _ := NewClient(ClientOptions{
		Dsn:       "http://whatever@really.com/1337",
		Transport: transport,
		Integrations: func(i []Integration) []Integration {
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
					Type:  "*sentry.customErr",
					Value: "wat",
					// No Stacktrace, because we can't tell where the error came
					// from and because we have a stack trace in the most recent
					// error in the chain.
				},
				{
					Type:       "*errors.withStack",
					Value:      "wat",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
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
					Type:  "*sentry.customErr",
					Value: "wat",
				},
				{
					Type:       "*sentry.customErrWithCause",
					Value:      "err",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
				},
			},
		},
		{
			name: "Go113Unwrap",
			err:  wrappedError{original: errors.New("original")},
			want: []Exception{
				{
					Type:  "*errors.errorString",
					Value: "original",
				},
				{
					Type:       "sentry.wrappedError",
					Value:      "wrapped: original",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
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
			Version:      Version,
			Integrations: []string{},
			Packages: []SdkPackage{
				{
					// FIXME: name format doesn't follow spec in
					// https://docs.sentry.io/development/sdk-dev/event-payloads/sdk/
					Name:    "sentry-go",
					Version: Version,
				},
				// TODO: perhaps the list of packages is incomplete or there
				// should not be any package at all. We may include references
				// to used integrations like http, echo, gin, etc.
			},
		},
	}
	got := transport.lastEvent
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestCaptureEventShouldSendEventWithProvidedError(t *testing.T) {
	client, scope, transport := setupClientTest()
	event := NewEvent()
	event.Message = "event message"
	client.CaptureEvent(event, nil, scope)
	assertEqual(t, transport.lastEvent.Message, "event message")
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

	client.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
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
	client.options.BeforeSend = func(event *Event, hint *EventHint) *Event {
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
