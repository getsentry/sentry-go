package sentry

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TraceIDFromHex(s string) TraceID {
	var id TraceID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func SpanIDFromHex(s string) SpanID {
	var id SpanID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func TestSpanMarshalJSON(t *testing.T) {
	s := &Span{}
	testMarshalJSONOmitEmptyParentSpanID(t, s)
}

func TestSpanStatusMarshalJSON(t *testing.T) {
	tests := map[SpanStatus]string{
		SpanStatus(42):             `null`,
		SpanStatusUndefined:        `null`,
		SpanStatusOK:               `"ok"`,
		SpanStatusDeadlineExceeded: `"deadline_exceeded"`,
		SpanStatusCanceled:         `"cancelled"`,
	}
	for s, want := range tests {
		s, want := s, want
		t.Run(fmt.Sprintf("SpanStatus(%d)", s), func(t *testing.T) {
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}

func TestTraceContextMarshalJSON(t *testing.T) {
	tc := &TraceContext{}
	testMarshalJSONOmitEmptyParentSpanID(t, tc)
}

func testMarshalJSONOmitEmptyParentSpanID(t *testing.T, v interface{}) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("parent_span_id")) {
		t.Fatalf("unwanted parent_span_id: %s", b)
	}
	id := reflect.ValueOf(SpanIDFromHex("c7b73e77a3734fee"))
	reflect.ValueOf(v).Elem().FieldByName("ParentSpanID").Set(id)
	b, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("parent_span_id")) {
		t.Fatalf("missing parent_span_id: %s", b)
	}
}
