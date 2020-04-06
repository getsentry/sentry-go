package sentry

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMarshalJSON(t *testing.T) {
	tests := []struct {
		in  interface{}
		out string
	}{
		// TODO: eliminate empty struct fields from serialization of empty event.
		// Only *Event implements json.Marshaler.
		// {Event{}, `{"sdk":{},"user":{}}`},
		{&Event{}, `{"sdk":{},"user":{}}`},
		// Only *Breadcrumb implements json.Marshaler.
		// {Breadcrumb{}, `{}`},
		{&Breadcrumb{}, `{}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run("", func(t *testing.T) {
			want := tt.out
			b, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("JSON serialization mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
