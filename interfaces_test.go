package sentry

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		{input: User{Segment: "My Segment"}, want: false},
		{input: User{Data: map[string]string{"foo": "bar"}}, want: false},
		{input: User{ID: "foo", Email: "foo@example.com", IPAddress: "127.0.0.1", Username: "My Username", Name: "My Name", Segment: "My Segment", Data: map[string]string{"foo": "bar"}}, want: false},
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
		{input: User{Segment: "My Segment"}, want: `{"segment":"My Segment"}`},
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

	const payload = `{"test_data": true}`
	r := httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload))
	r.Header.Add("Authorization", "Bearer 1234567890")
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
			"Authorization":   "Bearer 1234567890",
			"Cookie":          "foo=bar",
			"Host":            "example.com",
			"X-Forwarded-For": "127.0.0.1",
			"X-Real-Ip":       "127.0.0.1",
			"Some-Header":     "some-header value",
		},
		Env: map[string]string{
			"REMOTE_ADDR": "192.0.2.1",
			"REMOTE_PORT": "1234",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Request mismatch (-want +got):\n%s", diff)
	}
}

func TestNewRequestWithNoPII(t *testing.T) {
	const payload = `{"test_data": true}`
	r := httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload))
	r.Header.Add("Authorization", "Bearer 1234567890")
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
