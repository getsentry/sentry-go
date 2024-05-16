package sentry

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLocalAggregator(t *testing.T) {
	la := NewLocalAggregator()
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test"}, 2.0)
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test"}, 5.0)

	// // different tags
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test", "env": "dev"}, 3.0)

	want := map[string]map[string]MetricSummary{
		"d:function:millisecond": {
			"d:function:millisecondfoo:bar,route:/test": {
				min:   2,
				max:   5,
				sum:   7,
				count: 2,
			},
			"d:function:millisecondenv:dev,foo:bar,route:/test": {
				min:   3,
				max:   3,
				sum:   3,
				count: 1,
			},
		},
	}

	if diff := cmp.Diff(la.metricsSummary, want, cmp.AllowUnexported(MetricSummary{})); diff != "" {
		t.Errorf("Context mismatch (-want +got):\n%s", diff)
	}
}
