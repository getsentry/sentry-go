package sentry

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "allowed characters",
			key:  "test.metric-1",
			want: "test.metric-1",
		},
		{
			name: "forbidden characters",
			key:  "@test.me^tri'@c-1{}[]",
			want: "_test.me_tri_c-1_",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(sanitizeKey(test.key), test.want); diff != "" {
				t.Errorf("Context mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSanitizeValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "allowed characters",
			value: "test.metric-1",
			want:  "test.metric-1",
		},
		{
			name:  "forbidden characters",
			value: "@test.me^tri'+@c-1{}[]",
			want:  "@test.me_tri_@c-1{}[]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(sanitizeValue(test.value), test.want); diff != "" {
				t.Errorf("Context mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSerializeTags(t *testing.T) {
	tests := []struct {
		name   string
		metric abstractMetric
		want   string
	}{
		{
			name: "normal tags",
			metric: abstractMetric{
				tags: map[string]string{"tag1": "val1", "tag2": "val2"},
			},
			want: "tag1:val1,tag2:val2",
		},
		{
			name: "empty tags",
			metric: abstractMetric{
				tags: map[string]string{},
			},
			want: "",
		},
		{
			name: "un-sanitized tags",
			metric: abstractMetric{
				tags: map[string]string{"@env": "pro+d", "vers^^ion": `\release@`},
			},
			want: "_env:pro_d,vers_ion:_release@",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.metric.serializeTags(), test.want); diff != "" {
				t.Errorf("Context mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
