package sentry

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/google/go-cmp/cmp"
)

var (
	update   = flag.Bool("update", false, "update .golden files")
	generate = flag.Bool("gen", false, "generate missing .golden files")
)

func TestUserIsEmpty(t *testing.T) {
	tests := []struct {
		input User
		want  bool
	}{
		{input: User{}, want: true},
		{input: User{ID: "foo"}, want: false},
		{input: User{Email: "foo@example.com"}, want: false},
		{input: User{IPAddress: "127.0.0.1"}, want: false},
		{input: User{Username: "My Username"}, want: false},
		{input: User{Name: "My Name"}, want: false},
		{input: User{Data: map[string]string{"foo": "bar"}}, want: false},
		{input: User{ID: "foo", Email: "foo@example.com", IPAddress: "127.0.0.1", Username: "My Username", Name: "My Name", Data: map[string]string{"foo": "bar"}}, want: false},
		// Edge cases
		{input: User{Data: map[string]string{}}, want: true},   // Empty but non-nil map should be empty
		{input: User{ID: "   ", Username: "   "}, want: false}, // Whitespace-only fields should not be empty
	}

	for _, test := range tests {
		assertEqual(t, test.input.IsEmpty(), test.want)
	}
}

func TestUserMarshalJson(t *testing.T) {
	tests := []struct {
		input User
		want  string
	}{
		{input: User{}, want: `{}`},
		{input: User{ID: "foo"}, want: `{"id":"foo"}`},
		{input: User{Email: "foo@example.com"}, want: `{"email":"foo@example.com"}`},
		{input: User{IPAddress: "127.0.0.1"}, want: `{"ip_address":"127.0.0.1"}`},
		{input: User{Username: "My Username"}, want: `{"username":"My Username"}`},
		{input: User{Name: "My Name"}, want: `{"name":"My Name"}`},
		{input: User{Data: map[string]string{"foo": "bar"}}, want: `{"data":{"foo":"bar"}}`},
	}

	for _, test := range tests {
		got, err := json.Marshal(test.input)
		if err != nil {
			t.Fatal(err)
		}

		assertEqual(t, string(got), test.want)
	}
}

func TestNewRequest(t *testing.T) {
	currentHub.BindClient(&Client{
		options: ClientOptions{
			SendDefaultPII: true,
		},
	})
	// Unbind the client afterwards, to not affect other tests
	defer currentHub.stackTop().SetClient(nil)

	t.Run("standard request", func(t *testing.T) {
		const payload = `{"test_data": true}`
		r := httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload))
		r.Header.Add("Authorization", "Bearer 1234567890")
		r.Header.Add("Proxy-Authorization", "Bearer 123")
		r.Header.Add("Cookie", "foo=bar")
		r.Header.Add("X-Forwarded-For", "127.0.0.1")
		r.Header.Add("X-Real-Ip", "127.0.0.1")
		r.Header.Add("Some-Header", "some-header value")

		got := NewRequest(r)
		want := &Request{
			URL:         "http://example.com/test/",
			Method:      "POST",
			Data:        "",
			QueryString: "q=sentry",
			Cookies:     "foo=bar",
			Headers: map[string]string{
				"Authorization":       "Bearer 1234567890",
				"Proxy-Authorization": "Bearer 123",
				"Cookie":              "foo=bar",
				"Host":                "example.com",
				"X-Forwarded-For":     "127.0.0.1",
				"X-Real-Ip":           "127.0.0.1",
				"Some-Header":         "some-header value",
			},
			Env: map[string]string{
				"REMOTE_ADDR": "192.0.2.1",
				"REMOTE_PORT": "1234",
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Request mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("request with TLS", func(t *testing.T) {
		r := httptest.NewRequest("POST", "https://example.com/test", nil)
		r.TLS = &tls.ConnectionState{} // Simulate TLS connection

		got := NewRequest(r)

		if !strings.HasPrefix(got.URL, "https://") {
			t.Errorf("Request with TLS should have HTTPS URL, got %s", got.URL)
		}
	})

	t.Run("request with X-Forwarded-Proto header", func(t *testing.T) {
		r := httptest.NewRequest("POST", "http://example.com/test", nil)
		r.Header.Set("X-Forwarded-Proto", "https")

		got := NewRequest(r)

		if !strings.HasPrefix(got.URL, "https://") {
			t.Errorf("Request with X-Forwarded-Proto: https should have HTTPS URL, got %s", got.URL)
		}
	})

	t.Run("request with malformed RemoteAddr", func(t *testing.T) {
		r := httptest.NewRequest("POST", "http://example.com/test", nil)
		r.RemoteAddr = "malformed-address" // Invalid format

		got := NewRequest(r)

		if got.Env != nil {
			t.Error("Request with malformed RemoteAddr should not set Env")
		}
	})
}

func TestNewRequestWithNoPII(t *testing.T) {
	const payload = `{"test_data": true}`
	r := httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload))
	r.Header.Add("Authorization", "Bearer 1234567890")
	r.Header.Add("Proxy-Authorization", "Bearer 123")
	r.Header.Add("Cookie", "foo=bar")
	r.Header.Add("X-Forwarded-For", "127.0.0.1")
	r.Header.Add("X-Real-Ip", "127.0.0.1")
	r.Header.Add("Some-Header", "some-header value")

	got := NewRequest(r)
	want := &Request{
		URL:         "http://example.com/test/",
		Method:      "POST",
		Data:        "",
		QueryString: "q=sentry",
		Cookies:     "",
		Headers: map[string]string{
			"Host":        "example.com",
			"Some-Header": "some-header value",
		},
		Env: nil,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Request mismatch (-want +got):\n%s", diff)
	}
}

func TestEventMarshalJSON(t *testing.T) {
	event := NewEvent()
	event.Spans = []*Span{{
		TraceID:      TraceIDFromHex("d6c4f03650bd47699ec65c84352b6208"),
		SpanID:       SpanIDFromHex("1cc4b26ab9094ef0"),
		ParentSpanID: SpanIDFromHex("442bd97bbe564317"),
		StartTime:    time.Unix(8, 0).UTC(),
		EndTime:      time.Unix(10, 0).UTC(),
		Status:       SpanStatusOK,
	}}
	event.StartTime = time.Unix(7, 0).UTC()
	event.Timestamp = time.Unix(14, 0).UTC()

	got, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	// Non-transaction event should not have fields Spans and StartTime
	want := `{"sdk":{},"user":{},"timestamp":"1970-01-01T00:00:14Z"}`

	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestEventWithDebugMetaMarshalJSON(t *testing.T) {
	event := NewEvent()
	event.DebugMeta = &DebugMeta{
		SdkInfo: &DebugMetaSdkInfo{
			SdkName:           "test",
			VersionMajor:      1,
			VersionMinor:      2,
			VersionPatchlevel: 3,
		},
		Images: []DebugMetaImage{
			{
				Type:        "macho",
				ImageAddr:   "0xabcd0000",
				ImageSize:   32768,
				DebugID:     "42DB5B96-5144-4079-BE09-45E2142CA3E5",
				DebugFile:   "foo.dSYM",
				CodeID:      "A7AF6477-9130-4EB7-ADFE-AD0F57001DBD",
				CodeFile:    "foo.dylib",
				ImageVmaddr: "0x0",
				Arch:        "arm64",
			},
			{
				Type: "proguard",
				UUID: "982E62D4-6493-4E43-864B-6523C79C7064",
			},
		},
	}

	got, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"sdk":{},"user":{},` +
		`"debug_meta":{` +
		`"sdk_info":{"sdk_name":"test","version_major":1,"version_minor":2,"version_patchlevel":3},` +
		`"images":[` +
		`{"type":"macho",` +
		`"image_addr":"0xabcd0000",` +
		`"image_size":32768,` +
		`"debug_id":"42DB5B96-5144-4079-BE09-45E2142CA3E5",` +
		`"debug_file":"foo.dSYM",` +
		`"code_id":"A7AF6477-9130-4EB7-ADFE-AD0F57001DBD",` +
		`"code_file":"foo.dylib",` +
		`"image_vmaddr":"0x0",` +
		`"arch":"arm64"` +
		`},` +
		`{"type":"proguard","uuid":"982E62D4-6493-4E43-864B-6523C79C7064"}` +
		`]}}`

	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

type withCause struct {
	msg   string
	cause error
}

func (w *withCause) Error() string { return w.msg }
func (w *withCause) Cause() error  { return w.cause }

type customError struct {
	message string
}

func (e *customError) Error() string {
	return e.message
}

func TestSetException(t *testing.T) {
	testCases := map[string]struct {
		exception     error
		maxErrorDepth int
		expected      []Exception
	}{
		"Nil exception": {
			exception:     nil,
			maxErrorDepth: 5,
			expected:      []Exception{},
		},
		"Single error without unwrap": {
			exception:     errors.New("simple error"),
			maxErrorDepth: 1,
			expected: []Exception{
				{
					Value:      "simple error",
					Type:       "*errors.errorString",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism:  nil,
				},
			},
		},
		"Nested errors with Unwrap": {
			exception:     fmt.Errorf("level 2: %w", fmt.Errorf("level 1: %w", errors.New("base error"))),
			maxErrorDepth: 3,
			expected: []Exception{
				{
					Value:      "base error",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "level 1: base error",
					Type:  "*fmt.wrapError",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value:      "level 2: level 1: base error",
					Type:       "*fmt.wrapError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: false,
					},
				},
			},
		},
		"Custom error types": {
			exception: &customError{
				message: "custom error message",
			},
			maxErrorDepth: 1,
			expected: []Exception{
				{
					Value:      "custom error message",
					Type:       "*sentry.customError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism:  nil,
				},
			},
		},
		"Combination of Unwrap and Cause": {
			exception: fmt.Errorf("outer error: %w", &withCause{
				msg:   "error with cause",
				cause: errors.New("the cause"),
			}),
			maxErrorDepth: 3,
			expected: []Exception{
				{
					Value:      "the cause",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismSourceCause,
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error with cause",
					Type:  "*sentry.withCause",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value:      "outer error: error with cause",
					Type:       "*fmt.wrapError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: false,
					},
				},
			},
		},
		"errors.Join with multiple errors": {
			exception:     errors.Join(errors.New("error 1"), errors.New("error 2"), errors.New("error 3")),
			maxErrorDepth: 5,
			expected: []Exception{
				{
					Value:      "error 3",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[2]",
						ExceptionID:      3,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error 2",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      2,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error 1",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value:      "error 1\nerror 2\nerror 3",
					Type:       "*errors.joinError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: true,
					},
				},
			},
		},
		"Nested errors.Join with fmt.Errorf": {
			exception:     fmt.Errorf("wrapper: %w", errors.Join(errors.New("error A"), errors.New("error B"))),
			maxErrorDepth: 5,
			expected: []Exception{
				{
					Value:      "error B",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      3,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error A",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error A\nerror B",
					Type:  "*errors.joinError",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value:      "wrapper: error A\nerror B",
					Type:       "*fmt.wrapError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: false,
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			e := &Event{}
			e.SetException(tc.exception, tc.maxErrorDepth)

			if len(e.Exception) != len(tc.expected) {
				t.Fatalf("Expected %d exceptions, got %d", len(tc.expected), len(e.Exception))
			}

			for i, exp := range tc.expected {
				if diff := cmp.Diff(exp, e.Exception[i]); diff != "" {
					t.Errorf("Event mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestMechanismMarshalJSON(t *testing.T) {
	mechanism := &Mechanism{
		Type:        "some type",
		Description: "some description",
		HelpLink:    "some help link",
		Data: map[string]interface{}{
			"some data":         "some value",
			"some numeric data": 12345,
		},
	}

	got, err := json.Marshal(mechanism)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"type":"some type","description":"some description","help_link":"some help link",` +
		`"exception_id":0,"data":{"some data":"some value","some numeric data":12345}}`

	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestMechanismMarshalJSON_withHandled(t *testing.T) {
	mechanism := &Mechanism{
		Type:        "some type",
		Description: "some description",
		HelpLink:    "some help link",
		Data: map[string]interface{}{
			"some data":         "some value",
			"some numeric data": 12345,
		},
	}
	mechanism.SetUnhandled()

	got, err := json.Marshal(mechanism)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"type":"some type","description":"some description","help_link":"some help link",` +
		`"handled":false,"exception_id":0,"data":{"some data":"some value","some numeric data":12345}}`

	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestStructSnapshots(t *testing.T) {
	testSpan := &Span{
		TraceID:      TraceIDFromHex("d6c4f03650bd47699ec65c84352b6208"),
		SpanID:       SpanIDFromHex("1cc4b26ab9094ef0"),
		ParentSpanID: SpanIDFromHex("442bd97bbe564317"),
		Description:  `SELECT * FROM user WHERE "user"."id" = {id}`,
		Op:           "db.sql",
		Tags: map[string]string{
			"function_name":  "get_users",
			"status_message": "MYSQL OK",
		},
		StartTime: time.Unix(0, 0).UTC(),
		EndTime:   time.Unix(5, 0).UTC(),
		Status:    SpanStatusOK,
		Data: map[string]interface{}{
			"related_ids":  []uint{12312342, 76572, 4123485},
			"aws_instance": "ca-central-1",
		},
	}

	testCases := []struct {
		testName     string
		sentryStruct interface{}
	}{
		{
			testName:     "span",
			sentryStruct: testSpan,
		},
		{
			testName: "error_event",
			sentryStruct: &Event{
				Message:     "event message",
				Environment: "production",
				EventID:     EventID("0123456789abcdef"),
				Fingerprint: []string{"abcd"},
				Level:       LevelError,
				Platform:    "myplatform",
				Release:     "myrelease",
				Sdk: SdkInfo{
					Name:         "sentry.go",
					Version:      "0.0.1",
					Integrations: []string{"gin", "iris"},
					Packages: []SdkPackage{{
						Name:    "sentry-go",
						Version: "0.0.1",
					}},
				},
				ServerName:  "myhost",
				Timestamp:   time.Unix(5, 0).UTC(),
				Transaction: "mytransaction",
				User:        User{ID: "foo"},
				Breadcrumbs: []*Breadcrumb{{
					Data: map[string]interface{}{
						"data_key": "data_val",
					},
				}},
				Extra: map[string]interface{}{
					"extra_key": "extra_val",
				},
				Contexts: map[string]Context{
					"context_key": {
						"context_key": "context_val",
					},
				},
			},
		},
		{
			testName: "transaction_event",
			sentryStruct: &Event{
				Type:      transactionType,
				Spans:     []*Span{testSpan},
				StartTime: time.Unix(3, 0).UTC(),
				Timestamp: time.Unix(5, 0).UTC(),
				Contexts: map[string]Context{
					"trace": TraceContext{
						TraceID:     TraceIDFromHex("90d57511038845dcb4164a70fc3a7fdb"),
						SpanID:      SpanIDFromHex("f7f3fd754a9040eb"),
						Op:          "http.GET",
						Description: "description",
						Status:      SpanStatusOK,
					}.Map(),
				},
			},
		},
	}

	for _, test := range testCases {
		test := test
		t.Run(test.testName, func(t *testing.T) {
			got, err := json.MarshalIndent(test.sentryStruct, "", "    ")
			if err != nil {
				t.Error(err)
			}

			golden := filepath.Join(".", "testdata", fmt.Sprintf("%s.golden", test.testName))
			if *update {
				err := os.WriteFile(golden, got, 0600)
				if err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("struct %s mismatch (-want +got):\n%s", test.testName, diff)
			}
		})
	}
}

func TestEvent_ToCategory(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		want      ratelimit.Category
	}{
		{"error", "", ratelimit.CategoryError},
		{"transaction", transactionType, ratelimit.CategoryTransaction},
		{"log", logEvent.Type, ratelimit.CategoryLog},
		{"checkin", checkInType, ratelimit.CategoryMonitor},
		{"unknown", "foobar", ratelimit.CategoryUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Event{Type: tc.eventType}
			got := e.toCategory()
			if got != tc.want {
				t.Errorf("Type %q: got %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestEvent_ToEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		event     *Event
		dsn       *protocol.Dsn
		wantError bool
	}{
		{
			name: "basic event",
			event: &Event{
				EventID:   "12345678901234567890123456789012",
				Message:   "test message",
				Level:     LevelError,
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			dsn:       nil,
			wantError: false,
		},
		{
			name: "event with attachments",
			event: &Event{
				EventID:   "12345678901234567890123456789012",
				Message:   "test message",
				Level:     LevelError,
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Attachments: []*Attachment{
					{
						Filename:    "test.txt",
						ContentType: "text/plain",
						Payload:     []byte("test content"),
					},
				},
			},
			dsn:       nil,
			wantError: false,
		},
		{
			name: "transaction event",
			event: &Event{
				EventID:     "12345678901234567890123456789012",
				Type:        "transaction",
				Transaction: "test transaction",
				StartTime:   time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Timestamp:   time.Date(2023, 1, 1, 12, 0, 1, 0, time.UTC),
			},
			dsn:       nil,
			wantError: false,
		},
		{
			name: "check-in event",
			event: &Event{
				EventID: "12345678901234567890123456789012",
				Type:    "check_in",
				CheckIn: &CheckIn{
					ID:          "checkin123",
					MonitorSlug: "test-monitor",
					Status:      CheckInStatusOK,
					Duration:    5 * time.Second,
				},
			},
			dsn:       nil,
			wantError: false,
		},
		{
			name: "log event",
			event: &Event{
				EventID: "12345678901234567890123456789012",
				Type:    "log",
				Logs: []Log{
					{
						Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
						Level:     LogLevelInfo,
						Body:      "test log message",
					},
				},
			},
			dsn:       nil,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope, err := tt.event.ToEnvelope(tt.dsn)

			if (err != nil) != tt.wantError {
				t.Errorf("ToEnvelope() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return // Expected error, nothing more to check
			}

			// Basic envelope validation
			if envelope == nil {
				t.Error("ToEnvelope() returned nil envelope")
				return
			}

			if envelope.Header == nil {
				t.Error("Envelope header is nil")
				return
			}

			if envelope.Header.EventID != string(tt.event.EventID) {
				t.Errorf("Expected EventID %s, got %s", tt.event.EventID, envelope.Header.EventID)
			}

			// Check that items were created
			expectedItems := 1 // Main event item
			if tt.event.Attachments != nil {
				expectedItems += len(tt.event.Attachments)
			}

			if len(envelope.Items) != expectedItems {
				t.Errorf("Expected %d items, got %d", expectedItems, len(envelope.Items))
			}

			// Verify the envelope can be serialized
			data, err := envelope.Serialize()
			if err != nil {
				t.Errorf("Failed to serialize envelope: %v", err)
			}

			if len(data) == 0 {
				t.Error("Serialized envelope is empty")
			}
		})
	}
}

func TestEvent_ToEnvelopeWithTime(t *testing.T) {
	event := &Event{
		EventID:   "12345678901234567890123456789012",
		Message:   "test message",
		Level:     LevelError,
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	sentAt := time.Date(2023, 1, 1, 15, 0, 0, 0, time.UTC)
	envelope, err := event.ToEnvelopeWithTime(nil, sentAt)

	if err != nil {
		t.Errorf("ToEnvelopeWithTime() error = %v", err)
		return
	}

	if envelope == nil {
		t.Error("ToEnvelopeWithTime() returned nil envelope")
		return
	}

	if envelope.Header == nil {
		t.Error("Envelope header is nil")
		return
	}

	if !envelope.Header.SentAt.Equal(sentAt) {
		t.Errorf("Expected SentAt %v, got %v", sentAt, envelope.Header.SentAt)
	}
}

func TestEvent_ToEnvelope_FallbackOnMarshalError(t *testing.T) {
	unmarshalableFunc := func() string { return "test" }

	event := &Event{
		EventID:   "12345678901234567890123456789012",
		Message:   "test message with fallback",
		Level:     LevelError,
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Extra: map[string]interface{}{
			"bad_data": unmarshalableFunc,
		},
	}

	envelope, err := event.ToEnvelope(nil)

	if err != nil {
		t.Errorf("ToEnvelope() should not error even with unmarshalable data, got: %v", err)
		return
	}

	if envelope == nil {
		t.Error("ToEnvelope() should not return a nil envelope")
		return
	}

	data, _ := envelope.Serialize()

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		t.Error("Expected at least 2 lines in serialized envelope")
		return
	}

	var eventData map[string]interface{}
	if err := json.Unmarshal([]byte(lines[2]), &eventData); err != nil {
		t.Errorf("Failed to unmarshal event data: %v", err)
		return
	}

	extra, exists := eventData["extra"].(map[string]interface{})
	if !exists {
		t.Error("Expected extra field after fallback")
		return
	}

	info, exists := extra["info"].(string)
	if !exists || !strings.Contains(info, "Could not encode original event as JSON") {
		t.Fatal("Expected fallback info message in extra field for ToEnvelopeItem")
	}
}

func TestEvent_ToEnvelopeItem_FallbackOnMarshalError(t *testing.T) {
	unmarshalableFunc := func() string { return "test" }

	event := &Event{
		EventID:   "12345678901234567890123456789012",
		Message:   "test message with fallback",
		Level:     LevelError,
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Extra: map[string]interface{}{
			"bad_data": unmarshalableFunc,
		},
	}

	item, err := event.ToEnvelopeItem()
	if err != nil {
		t.Errorf("ToEnvelopeItem() should not error even with unmarshalable data, got: %v", err)
		return
	}
	if item == nil {
		t.Fatal("ToEnvelopeItem() returned nil item")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("Failed to unmarshal item payload: %v", err)
	}

	extra, exists := payload["extra"].(map[string]interface{})
	if !exists {
		t.Fatal("Expected extra field after fallback in ToEnvelopeItem")
	}

	info, exists := extra["info"].(string)
	if !exists || !strings.Contains(info, "Could not encode original event as JSON") {
		t.Fatal("Expected fallback info message in extra field for ToEnvelopeItem")
	}
}

func TestLog_ToEnvelopeItem_And_Getters(t *testing.T) {
	ts := time.Unix(1700000000, 500_000_000).UTC()
	trace := TraceIDFromHex("d6c4f03650bd47699ec65c84352b6208")
	l := &Log{
		Timestamp: ts,
		TraceID:   trace,
		Level:     LogLevelInfo,
		Severity:  LogSeverityInfo,
		Body:      "hello world",
		Attributes: map[string]Attribute{
			"k1": {Value: "v1", Type: AttributeString},
			"k2": {Value: int64(42), Type: AttributeInt},
		},
	}

	item, err := l.ToEnvelopeItem()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item == nil || item.Header == nil {
		t.Fatal("expected non-nil envelope item and header")
	}
	if item.Header.Type != protocol.EnvelopeItemTypeLog {
		t.Fatalf("expected log item type, got %q", item.Header.Type)
	}

	var payload struct {
		Timestamp  *float64                         `json:"timestamp,omitempty"`
		TraceID    string                           `json:"trace_id,omitempty"`
		Level      string                           `json:"level"`
		Severity   int                              `json:"severity_number,omitempty"`
		Body       string                           `json:"body,omitempty"`
		Attributes map[string]protocol.LogAttribute `json:"attributes,omitempty"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Timestamp == nil {
		t.Fatal("expected timestamp to be set")
	}
	if *payload.Timestamp < 1.7e9 || *payload.Timestamp > 1.700000001e9 {
		t.Fatalf("unexpected timestamp: %v", *payload.Timestamp)
	}
	if payload.TraceID != trace.String() {
		t.Fatalf("unexpected trace id: %q", payload.TraceID)
	}
	if payload.Level != string(LogLevelInfo) {
		t.Fatalf("unexpected level: %q", payload.Level)
	}
	if payload.Severity != LogSeverityInfo {
		t.Fatalf("unexpected severity: %d", payload.Severity)
	}
	if payload.Body != "hello world" {
		t.Fatalf("unexpected body: %q", payload.Body)
	}
	if payload.Attributes["k1"].Type != string(AttributeString) || payload.Attributes["k1"].Value != "v1" {
		t.Fatalf("unexpected attribute k1: %+v", payload.Attributes["k1"])
	}
	if payload.Attributes["k2"].Type != string(AttributeInt) || payload.Attributes["k2"].Value != float64(42) {
		t.Fatalf("unexpected attribute k2: %+v", payload.Attributes["k2"])
	}

	if l.GetCategory() != ratelimit.CategoryLog {
		t.Fatalf("unexpected category: %v", l.GetCategory())
	}
	if l.GetEventID() != "" {
		t.Fatalf("expected empty event id, got %q", l.GetEventID())
	}
	if l.GetSdkInfo() != nil {
		t.Fatal("expected nil sdk info for logs")
	}
	if dsc := l.GetDynamicSamplingContext(); dsc != nil {
		t.Fatalf("expected nil DSC for logs, got: %+v", dsc)
	}
}
