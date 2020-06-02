package sentry

import (
	"bytes"
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

var update = flag.Bool("update", false, "update .golden files") //nolint: gochecknoglobals

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

func TestMarshalStruct(t *testing.T) {
	testSpan := &Span{
		TraceID:      "d6c4f03650bd47699ec65c84352b6208",
		SpanID:       "1cc4b26ab9094ef0",
		ParentSpanID: "442bd97bbe564317",
		Description:  `SELECT * FROM user WHERE "user"."id" = {id}`,
		Op:           "db.sql",
		Tags: map[string]string{
			"function_name":  "get_users",
			"status_message": "MYSQL OK",
		},
		StartTimestamp: time.Unix(0, 0).UTC(),
		EndTimestamp:   time.Unix(5, 0).UTC(),
		Status:         "ok",
	}

	testCases := []struct {
		testName     string
		sentryStruct interface{}
	}{
		{
			testName:     "span",
			sentryStruct: testSpan,
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
					t.Error(err)
				}
			}

			want, err := ioutil.ReadFile(golden)
			if err != nil {
				t.Error(err)
			}

			if !bytes.Equal(got, want) {
				t.Errorf("struct %s\n\tgot:\n%s\n\twant:\n%s", test.testName, got, want)
			}
		})
	}
}
