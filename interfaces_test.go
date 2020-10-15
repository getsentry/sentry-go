package sentry

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http/httptest"
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

func TestNewRequest(t *testing.T) {
	const payload = `{"test_data": true}`
	got := NewRequest(httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload)))
	want := &Request{
		URL:         "http://example.com/test/",
		Method:      "POST",
		Data:        "",
		QueryString: "q=sentry",
		Cookies:     "",
		Headers: map[string]string{
			"Host": "example.com",
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
				Contexts: map[string]interface{}{
					"context_key": "context_val",
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
				Contexts: map[string]interface{}{
					"trace": &TraceContext{
						TraceID:     TraceIDFromHex("90d57511038845dcb4164a70fc3a7fdb"),
						SpanID:      SpanIDFromHex("f7f3fd754a9040eb"),
						Op:          "http.GET",
						Description: "description",
						Status:      SpanStatusOK,
					},
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
				err := ioutil.WriteFile(golden, got, 0600)
				if err != nil {
					t.Fatal(err)
				}
			}

			want, err := ioutil.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("struct %s mismatch (-want +got):\n%s", test.testName, diff)
			}
		})
	}
}
