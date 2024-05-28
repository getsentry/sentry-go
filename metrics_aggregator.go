package sentry

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"time"
)

const rollupInSeconds = 10

type MetricsAggregator struct {
	// bucket into X second intervals
	rollupInSeconds int64
	maxWeight       uint
	flushInterval   uint8
	// The aggregator shifts its flushing by up to an entire rollup window to
	// avoid multiple clients trampling on end of a 10 second window as all the
	// buckets are anchored to multiples of ROLLUP seconds.  We randomize this
	// number once per aggregator boot to achieve some level of offsetting
	// across a fleet of deployed SDKs.  Relay itself will also apply independent
	// jittering.
	flushShift uint8
	// `timestamp -> [metric_key -> metric]`
	buckets            map[int64]map[string]Metric
	bucketsTotalWeight uint
}

func NewMetricsAggregator() MetricsAggregator {
	return MetricsAggregator{
		rollupInSeconds: rollupInSeconds,
		buckets:         make(map[int64]map[string]Metric),
		maxWeight:       100000,

		flushShift: uint8(math.Round(rollupInSeconds * rand.Float64())),
		//self._flush_shift = random.random() * self.ROLLUP_IN_SECONDS
		// TODO finish assignment
	}
}

func (ma *MetricsAggregator) add(
	ctx context.Context,
	ty string,
	key string,
	unit MetricUnit,
	tags map[string]string,
	timestamp time.Time,
	value interface{},
) error {

	bucketTimestamp := (timestamp.Unix() / ma.rollupInSeconds)
	serializedTags := ma.serializeTags(ctx, tags)
	h := md5.New()
	_, _ = io.WriteString(h, ty)
	_, _ = io.WriteString(h, unit.unit)
	_, _ = io.WriteString(h, serializedTags)
	bucketKey := hex.EncodeToString(h.Sum(nil))
	var previousWeight int
	var metric Metric

	if localBucket, ok := ma.buckets[bucketTimestamp]; !ok {
		// if there is no bucket for the given timestamp ....
		ma.buckets[bucketTimestamp] = make(map[string]Metric)
		m, err := buildMetric(ty, key, unit, tags, timestamp, value)
		if err != nil {
			return err
		}
		ma.buckets[bucketTimestamp][bucketKey] = m
		previousWeight = 0
		metric = m
	} else {
		// if there is a bucket for the the given metric (bucket key) ....
		if m, ok := localBucket[bucketKey]; ok {
			previousWeight = m.GetWeight()
			m.Add(value)
			metric = m
		} else {
			// if there is no bucket for the the given metric (bucket key) ....
			m, err := buildMetric(ty, key, unit, tags, timestamp, value)
			if err != nil {
				return err
			}
			localBucket[bucketKey] = m
			previousWeight = 0
			metric = m
		}

	}
	added := metric.GetWeight() - previousWeight
	ma.bucketsTotalWeight += uint(added)

	return nil
}

func (ma *MetricsAggregator) serializeTags(ctx context.Context, tags map[string]string) string {
	client := hubFromContext(ctx).Client()

	if client != nil {
		co := client.Options()
		if _, ok := tags["environment"]; !ok {
			tags["environment"] = co.Environment
		}
		if _, ok := tags["release"]; !ok {
			tags["release"] = co.Release
		}
		if _, ok := tags["transaction"]; !ok {
			if s := SpanFromContext(ctx); s != nil {
				tags["transaction"] = s.Name
			}
		}
	}

	return serializeTags(tags)
}

func buildMetric(
	ty string,
	key string,
	unit MetricUnit,
	tags map[string]string,
	timestamp time.Time,
	value interface{}) (Metric, error) {

	switch ty {
	case "c":
		return NewCounterMetric(key, unit, tags, timestamp.Unix(), value.(float64)), nil
	case "d":
		return NewDistributionMetric(key, unit, tags, timestamp.Unix(), value.(float64)), nil
	case "g":
		return NewGaugeMetric(key, unit, tags, timestamp.Unix(), value.(float64)), nil
	case "s":
		return NewSetMetric(key, unit, tags, timestamp.Unix(), value.(int)), nil
	default:
		// we should never end up in this branch as buildMetric is called by the higher
		// level APIs. Still, golang requires us to be exhaustive so a default case
		// must be defined.
		return nil, errors.New("no such metric type exist")
	}
}

type MetricSummary struct {
	Min   float64           `json:"min"`
	Max   float64           `json:"max"`
	Sum   float64           `json:"sum"`
	Count float64           `json:"count"`
	Tags  map[string]string `json:"tags"`
}

func (ms *MetricSummary) Add(value float64) {
	ms.Min = math.Min(ms.Min, value)
	ms.Max = math.Max(ms.Max, value)
	ms.Sum += value
	ms.Count++
}

type LocalAggregator struct {
	// [mri --> [bucket_key --> metric_summary]]
	MetricsSummary map[string]map[string]MetricSummary
}

func NewLocalAggregator() LocalAggregator {
	return LocalAggregator{
		MetricsSummary: make(map[string]map[string]MetricSummary),
	}
}

func (la *LocalAggregator) Add(
	ty string,
	key string,
	unit MetricUnit,
	tags map[string]string,
	value interface{},
) {
	mri := fmt.Sprintf("%s:%s@%s", ty, key, unit.unit)
	bucketKey := fmt.Sprintf("%s%s", mri, serializeTags(tags))
	val := value.(float64)

	if mriBucket, ok := la.MetricsSummary[mri]; ok {
		if metricSummary, ok := mriBucket[bucketKey]; ok {
			metricSummary.Add(val)
			la.MetricsSummary[mri][bucketKey] = metricSummary
			return
		}
	}
	// else if the bucket does not exist, initialize it
	if la.MetricsSummary[mri] == nil {
		la.MetricsSummary[mri] = make(map[string]MetricSummary)
	}
	la.MetricsSummary[mri][bucketKey] = MetricSummary{
		Min:   val,
		Max:   val,
		Sum:   val,
		Count: 1,
		Tags:  tags,
	}
}

func (la LocalAggregator) MarshalJSON() ([]byte, error) {
	summary := make(map[string][]MetricSummary)

	for mri, metricSummaries := range la.MetricsSummary {
		for _, metricSummary := range metricSummaries {
			if _, ok := summary[mri]; !ok {
				summary[mri] = make([]MetricSummary, 0, len(metricSummaries))
			}
			summary[mri] = append(summary[mri], metricSummary)
		}
	}

	return json.Marshal(summary)
}

func Increment(ctx context.Context, key string, value float64, unit MetricUnit, tags map[string]string, timestamp *time.Time) {
	client := CurrentHub().Client()
	if client != nil && client.metricsAggregator != nil {
		if timestamp == nil {
			t := time.Now()
			timestamp = &t
		}
		client.metricsAggregator.add(ctx, "c", key, unit, tags, *timestamp, value)
	}
}

func Distribution(ctx context.Context, key string, value float64, unit MetricUnit, tags map[string]string, timestamp *time.Time) {
	client := CurrentHub().Client()
	if client != nil && client.metricsAggregator != nil {
		if timestamp == nil {
			t := time.Now()
			timestamp = &t
		}
		client.metricsAggregator.add(ctx, "d", key, unit, tags, *timestamp, value)
	}
}

func Gauge(ctx context.Context, key string, value float64, unit MetricUnit, tags map[string]string, timestamp *time.Time) {
	client := CurrentHub().Client()
	if client != nil && client.metricsAggregator != nil {
		if timestamp == nil {
			t := time.Now()
			timestamp = &t
		}
		client.metricsAggregator.add(ctx, "g", key, unit, tags, *timestamp, value)
	}
}

func Set(ctx context.Context, key string, value int, unit MetricUnit, tags map[string]string, timestamp *time.Time) {
	client := CurrentHub().Client()
	if client != nil && client.metricsAggregator != nil {
		if timestamp == nil {
			t := time.Now()
			timestamp = &t
		}
		client.metricsAggregator.add(ctx, "s", key, unit, tags, *timestamp, value)
	}
}

func SetString(ctx context.Context, key string, value string, unit MetricUnit, tags map[string]string, timestamp *time.Time) {
	client := CurrentHub().Client()
	if client != nil && client.metricsAggregator != nil {
		if timestamp == nil {
			t := time.Now()
			timestamp = &t
		}
		v := int(setStringKeyToInt(value))
		client.metricsAggregator.add(ctx, "s", key, unit, tags, *timestamp, v)
	}
}
