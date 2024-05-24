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
	"time"
)

type MetricsAggregator struct {
	rollupInSeconds int64
	maxWeight       uint
	flushInterval   uint8
	flushShift      uint8
	// `timestamp -> [metric_key -> metric]`
	buckets            map[int64]map[string]Metric
	bucketsTotalWeight uint
}

func (ma *MetricsAggregator) add(
	ctx context.Context,
	ty string,
	key string,
	unit MetricUnit,
	tags map[string]string,
	timestamp *time.Time,
	value interface{},
) error {
	if timestamp == nil {
		t := time.Now()
		timestamp = &t
	}

	bucketTimestamp := (timestamp.Unix() / ma.rollupInSeconds)
	serializedTags := ma.serializeTags(ctx, tags)
	h := md5.New()
	_, _ = io.WriteString(h, ty)
	_, _ = io.WriteString(h, unit.unit)
	_, _ = io.WriteString(h, serializedTags)
	bucketKey := hex.EncodeToString(h.Sum(nil))

	if localBucket, ok := ma.buckets[bucketTimestamp]; !ok {
		m, err := buildMetric(ty, key, unit, tags, timestamp, value)
		if err != nil {
			return err
		}
		ma.buckets[bucketTimestamp][bucketKey] = m
	} else {
		if m, ok := localBucket[bucketKey]; ok {
			m.Add(value)
			// TODO: set weight
		} else {
			m, err := buildMetric(ty, key, unit, tags, timestamp, value)
			if err != nil {
				return err
			}
			localBucket[bucketKey] = m
			// TODO: set weight
		}
		fmt.Println(localBucket)
	}
	// TODO

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
	timestamp *time.Time,
	value interface{}) (Metric, error) {
	f64, _ := value.(float64)
	switch ty {
	case "c":
		return NewCounterMetric(key, unit, tags, timestamp.Unix(), f64), nil
	case "d":
		return NewDistributionMetric(key, unit, tags, timestamp.Unix(), f64), nil
	case "g":
		return NewGaugeMetric(key, unit, tags, timestamp.Unix(), f64), nil
	case "s":
		if v, ok := value.(int); ok {
			return NewSetMetric[int](key, unit, tags, timestamp.Unix(), v), nil
		} else if v, ok := value.(string); ok {
			return NewSetMetric[string](key, unit, tags, timestamp.Unix(), int(setStringKeyToInt(v))), nil
		} else {
			return nil, errors.New("set metric only accept string or int values")
		}
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
	var val float64

	if mriBucket, ok := la.MetricsSummary[mri]; ok {
		if metricSummary, ok := mriBucket[bucketKey]; ok {
			switch ty {
			case "s":
				val = 1.0
			default:
				val = value.(float64)
			}
			metricSummary.Add(val)
			la.MetricsSummary[mri][bucketKey] = metricSummary
			return
		}
	}
	// else if the bucket does not exist, initialize it
	if la.MetricsSummary[mri] == nil {
		la.MetricsSummary[mri] = make(map[string]MetricSummary)
	}
	switch ty {
	case "s":
		val = 0.0
	default:
		val = value.(float64)
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
