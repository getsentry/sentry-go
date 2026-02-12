package testutils

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name string
		maps []map[string]string
		want map[string]string
	}{
		{
			name: "merge two maps without overlap",
			maps: []map[string]string{
				{"a": "1", "b": "2"},
				{"c": "3", "d": "4"},
			},
			want: map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"},
		},
		{
			name: "merge with overlapping keys - last wins",
			maps: []map[string]string{
				{"a": "first", "b": "2"},
				{"a": "second", "c": "3"},
				{"a": "third"},
			},
			want: map[string]string{"a": "third", "b": "2", "c": "3"},
		},
		{
			name: "merge with empty maps",
			maps: []map[string]string{
				{"a": "1"},
				{},
				{"b": "2"},
			},
			want: map[string]string{"a": "1", "b": "2"},
		},
		{
			name: "single map",
			maps: []map[string]string{
				{"a": "1", "b": "2"},
			},
			want: map[string]string{"a": "1", "b": "2"},
		},
		{
			name: "no maps",
			maps: []map[string]string{},
			want: map[string]string{},
		},
		{
			name: "all empty maps",
			maps: []map[string]string{{}, {}, {}},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeMaps(tt.maps...)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("MergeMaps() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMergeMaps_DifferentTypes(t *testing.T) {
	t.Run("map[string]int", func(t *testing.T) {
		m1 := map[string]int{"a": 1, "b": 2}
		m2 := map[string]int{"b": 3, "c": 4}
		want := map[string]int{"a": 1, "b": 3, "c": 4}

		got := MergeMaps(m1, m2)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("MergeMaps() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("map[int]string", func(t *testing.T) {
		m1 := map[int]string{1: "a", 2: "b"}
		m2 := map[int]string{2: "c", 3: "d"}
		want := map[int]string{1: "a", 2: "c", 3: "d"}

		got := MergeMaps(m1, m2)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("MergeMaps() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("map[string]interface{}", func(t *testing.T) {
		m1 := map[string]interface{}{"a": 1, "b": "string"}
		m2 := map[string]interface{}{"b": 42, "c": true}
		want := map[string]interface{}{"a": 1, "b": 42, "c": true}

		got := MergeMaps(m1, m2)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("MergeMaps() mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestMergeMaps_PreservesOriginal(t *testing.T) {
	m1 := map[string]string{"a": "1", "b": "2"}
	m2 := map[string]string{"b": "3", "c": "4"}

	original1 := make(map[string]string)
	for k, v := range m1 {
		original1[k] = v
	}
	original2 := make(map[string]string)
	for k, v := range m2 {
		original2[k] = v
	}

	_ = MergeMaps(m1, m2)

	if diff := cmp.Diff(original1, m1); diff != "" {
		t.Errorf("MergeMaps() modified first map (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(original2, m2); diff != "" {
		t.Errorf("MergeMaps() modified second map (-want +got):\n%s", diff)
	}
}
