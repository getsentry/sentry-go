package sentry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var (
	goReleaseDate = time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	utcMinusTwo   = time.FixedZone("UTC-2", -2*60*60)
)

func TestMarshalJSON(t *testing.T) {
	tests := []struct {
		in  any
		out string
	}{
		// TODO: eliminate empty struct fields from serialization of empty event.
		{&Event{}, `{"sdk":{}}`},
		{&Transaction{}, `{"sdk":{}}`},
		// Only *Breadcrumb implements json.Marshaler.
		// {Breadcrumb{}, `{}`},
		{&Breadcrumb{}, `{}`},
	}
	for _, tt := range tests {
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

func TestBreadcrumbMarshalJSON(t *testing.T) {
	tests := []*Breadcrumb{
		// complete
		{
			Type:     "default",
			Category: "sentryhttp",
			Message:  "breadcrumb message",
			Data: map[string]any{
				"key": "value",
			},
			Level:     LevelInfo,
			Timestamp: goReleaseDate,
		},
		// timestamp not in UTC
		{
			Data: map[string]any{
				"key": "value",
			},
			Timestamp: goReleaseDate.In(utcMinusTwo),
		},
		// missing timestamp
		{
			Message: "breadcrumb message",
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	for i, tt := range tests {
		t.Run("", func(t *testing.T) {
			defer buf.Reset()
			err := enc.Encode(tt)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "json", "breadcrumb", fmt.Sprintf("%03d.json", i))
			if *update {
				WriteGoldenFile(t, path, buf.Bytes())
			}
			got := buf.String()
			want := ReadOrGenerateGoldenFile(t, path, buf.Bytes())
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("JSON mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func WriteGoldenFile(t *testing.T, path string, bytes []byte) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0777)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, bytes, 0666)
	if err != nil {
		t.Fatal(err)
	}
}

func ReadOrGenerateGoldenFile(t *testing.T, path string, bytes []byte) string {
	t.Helper()
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if *generate {
			WriteGoldenFile(t, path, bytes)
			return string(bytes)
		}
		t.Fatalf("Missing golden file. Run `go test -args -gen` to generate it.")
	case err != nil:
		t.Fatal(err)
	}
	return string(b)
}
