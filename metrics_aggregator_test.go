package sentry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestLocalAggregator(t *testing.T) {
	la := NewLocalAggregator()
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test"}, 2.0)
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test"}, 5.0)

	// // different tags
	la.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar", "route": "/test", "env": "dev"}, 3.0)

	want := map[string]map[string]MetricSummary{
		"d:function@millisecond": {
			"d:function@millisecondfoo:bar,route:/test": {
				Min:   2,
				Max:   5,
				Sum:   7,
				Count: 2,
				Tags:  map[string]string{"foo": "bar", "route": "/test"},
			},
			"d:function@millisecondenv:dev,foo:bar,route:/test": {
				Min:   3,
				Max:   3,
				Sum:   3,
				Count: 1,
				Tags:  map[string]string{"foo": "bar", "route": "/test", "env": "dev"},
			},
		},
	}

	if diff := cmp.Diff(la.MetricsSummary, want, cmp.AllowUnexported(MetricSummary{})); diff != "" {
		t.Errorf("Context mismatch (-want +got):\n%s", diff)
	}
}

func TestLocalAggregatorMarshal(t *testing.T) {
	la1 := NewLocalAggregator()
	la1.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar"}, 2.0)
	la1.Add("d", "function", MilliSecond(), map[string]string{"foo": "bar"}, 5.0)

	b, err := json.Marshal(la1)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	have := string(b)
	want := `{"d:function@millisecond":[{"min":2,"max":5,"sum":7,"count":2,"tags":{"foo":"bar"}}]}`

	if diff := cmp.Diff(have, want); diff != "" {
		t.Errorf("Context mismatch (-want +got):\n%s", diff)
	}
}

func TestMetricAggregatorBucketsTotalWeight(t *testing.T) {
	ma := NewMetricsAggregator()
	ctx := context.Background()
	ma.add(ctx, "d", "key1", MilliSecond(), map[string]string{"foo": "bar"}, time.Now(), 1.0)
	ma.add(ctx, "d", "key1", MilliSecond(), map[string]string{"foo": "bar"}, time.Now(), 2.0)
	ma.add(ctx, "s", "key2", MilliSecond(), map[string]string{"foo": "bar"}, time.Now(), 5)

	if ma.bucketsTotalWeight != 3 {
		t.Fatalf("bucketsTotalWeight -> Expected: 3, Found: %d", ma.bucketsTotalWeight)
	}

	ma.add(ctx, "s", "key2", MilliSecond(), map[string]string{"foo": "bar"}, time.Now(), 5)

	// bucketsTotalWeight should still be equal to 3, since we've added to the set metric
	// the same value we added before, so no new insertion should have happened
	if ma.bucketsTotalWeight != 3 {
		t.Fatalf("bucketsTotalWeight -> Expected: 3, Found: %d", ma.bucketsTotalWeight)
	}
}
