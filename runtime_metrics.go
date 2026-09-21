package sentry

import (
	"context"
	"math"
	"runtime"
	runtime_metrics "runtime/metrics"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

// RuntimeMetricsConfig configures the runtime metrics integration.
// You may leave this empty to use the default values. For proper graceful
// shutdown, you should provide a context that is canceled when the program
// exits.
type RuntimeMetricsConfig struct {
	// Disabled disables the runtime metrics integration.
	Disabled bool
	// Interval is the interval at which the runtime metrics are collected.
	// Default is 30 seconds. You don't want to set this too low, as it will
	// trigger a lot of "stop-the-world" activity.
	Interval time.Duration
	// Context is the context that is used to stop the runtime metrics
	// integration. If you don't provide a context, the SDK will create
	// an empty background context.
	Context context.Context
	// CollectGCMetrics enables the collection of GC metrics.
	// Default is false (disabled).
	CollectGCMetrics bool

	// newTicker returns a channel that delivers ticks at the given interval
	// plus a function that stops them. nil means time.NewTicker. Tests inject
	// a deterministic ticker so they can drive collection without waiting on
	// the wall clock.
	newTicker func(time.Duration) (<-chan time.Time, func())
}

// Strongly typed metric keys
type runtimeMetricKeyMap struct {
	Key  string
	Unit string
	Type MetricType
}

// The following metrics are collected by the runtime metrics integration.
// This should be synced with the `runtimeMetricsSamples` variable.
var runtimeMetricsKeys = []runtimeMetricKeyMap{
	{"go.cpu.utilization", UnitRatio, MetricTypeGauge},
	{"go.memory.used.total", UnitByte, MetricTypeGauge},
	{"go.memory.used.heap.objects", UnitByte, MetricTypeGauge},
	{"go.memory.used.heap.free", UnitByte, MetricTypeGauge},
	{"go.memory.used.heap.unused", UnitByte, MetricTypeGauge},
	{"go.memory.used.heap.stacks", UnitByte, MetricTypeGauge},
	{"go.memory.used.other", UnitByte, MetricTypeGauge},
	{"go.memory.limit", UnitByte, MetricTypeGauge},
	{"go.goroutines.count", "goroutines", MetricTypeGauge},
	{"go.memory.allocated", UnitByte, MetricTypeCounter},
}

// The following metrics are collected by the runtime metrics integration.
// This should be synced with the `runtimeMetricsKeys` variable.
var runtimeMetricsSamples = []runtime_metrics.Sample{
	{Name: "/cpu/classes/total:cpu-seconds"},
	{Name: "/memory/classes/total:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/heap/free:bytes"},
	{Name: "/memory/classes/heap/unused:bytes"},
	{Name: "/memory/classes/heap/stacks:bytes"},
	{Name: "/memory/classes/other:bytes"},
	{Name: "/gc/gomemlimit:bytes"},
	{Name: "/sched/goroutines:goroutines"},
	{Name: "/gc/heap/allocs:bytes"},
}

// To ensure that the runtime metrics integration is only started once.
// From the Go docs:
//
// > It is safe to execute multiple Read calls concurrently, but their arguments
// > must share no underlying memory. When in doubt, create a new []Sample from
// > scratch, which is always safe, though may be inefficient.
var onceRuntimeMetrics = sync.Once{}

// A simple marker to guarantee that the runtime metrics integration is only
// started once.
var runtimeMetricsRunning = false

type cpuUtilizationTracker struct {
	lastCPUSeconds float64
	lastSampleTime time.Time

	// now returns the current time. nil means time.Now. Tests inject a
	// deterministic clock to advance wall-clock time explicitly.
	now func() time.Time
}

var maxProcs = float64(runtime.GOMAXPROCS(-1))

func (t *cpuUtilizationTracker) GetCPUUtilization(currentCPUSeconds float64) float64 {
	now := t.now
	if now == nil {
		now = time.Now
	}
	nowTime := now()

	// First sample — can't calculate yet
	if t.lastSampleTime.IsZero() {
		t.lastCPUSeconds = currentCPUSeconds
		t.lastSampleTime = nowTime
		return 0.0
	}

	// Calculate deltas
	cpuDelta := currentCPUSeconds - t.lastCPUSeconds
	wallClockDelta := nowTime.Sub(t.lastSampleTime).Seconds()

	// Normalize by GOMAXPROCS to get utilization percentage
	// cpuDelta represents CPU-seconds consumed across all GOMAXPROCS goroutines
	// wallClockDelta is real time that passed
	// Divide by GOMAXPROCS to account for parallel CPUs
	utilization := cpuDelta / (wallClockDelta * maxProcs)

	// Clamp to [0.0, 1.0] (shouldn't exceed unless there's jitter)
	if utilization > 1.0 {
		utilization = 1.0
	}
	if utilization < 0.0 {
		utilization = 0.0
	}

	// Update state for next sample
	t.lastCPUSeconds = currentCPUSeconds
	t.lastSampleTime = nowTime

	return utilization
}

// StartRuntimeMetrics starts the runtime metrics integration.
// This should be called using `go sentry.StartRuntimeMetrics(config)`.
// When invoked multiple times, the integration is only started once.
func StartRuntimeMetrics(config RuntimeMetricsConfig) {
	// ensure this would not crash the program
	defer func() {
		if r := recover(); r != nil {
			debuglog.Printf("panic during runtime metrics integration: %v", r)
		}
	}()

	if config.Disabled {
		return
	}

	if runtimeMetricsRunning {
		return
	}

	onceRuntimeMetrics.Do(func() {
		runtimeMetricsRunning = true
	})

	// Handle opt-in metrics
	if config.CollectGCMetrics {
		runtimeMetricsKeys = append(
			runtimeMetricsKeys,
			runtimeMetricKeyMap{"go.memory.gc.cycles", "cycles", MetricTypeCounter},
			runtimeMetricKeyMap{"go.memory.gc.pause", UnitSecond, MetricTypeDistribution},
		)

		runtimeMetricsSamples = append(
			runtimeMetricsSamples,
			runtime_metrics.Sample{Name: "/gc/cycles/total:gc-cycles"},
			runtime_metrics.Sample{Name: "/sched/pauses/total/gc:seconds"},
		)
	}

	// XXX(aldy505): Do we guard the `interval` when it's below or over a certain threshold?
	// Javascript SDK defaults to 30 seconds.
	if config.Interval <= 0 {
		config.Interval = time.Second * 30
	}

	hub := CurrentHub()

	if config.Context == nil {
		config.Context = SetHubOnContext(context.Background(), hub)
	}

	meter := NewMeter(config.Context)

	if config.newTicker == nil {
		config.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
			ticker := time.NewTicker(d)
			return ticker.C, ticker.Stop
		}
	}
	tickCh, stopTicker := config.newTicker(config.Interval)
	defer stopTicker()
	cpuTracker := cpuUtilizationTracker{}

	for {
		select {
		case <-config.Context.Done():
			return
		case <-tickCh:
			runtime_metrics.Read(runtimeMetricsSamples)
			for i, sample := range runtimeMetricsSamples {
				switch sample.Value.Kind() {
				case runtime_metrics.KindFloat64:
					value := sample.Value.Float64()
					if sample.Name == "/cpu/classes/total:cpu-seconds" {
						value = cpuTracker.GetCPUUtilization(value)
					}
					switch runtimeMetricsKeys[i].Type {
					case MetricTypeGauge:
						meter.Gauge(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
					case MetricTypeCounter:
						meter.Count(runtimeMetricsKeys[i].Key, int64(value), WithUnit(runtimeMetricsKeys[i].Unit))
					case MetricTypeDistribution:
						meter.Distribution(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
					}
				case runtime_metrics.KindUint64:
					value := float64(sample.Value.Uint64())
					if sample.Name == "/cpu/classes/total:cpu-seconds" {
						value = cpuTracker.GetCPUUtilization(value)
					}
					switch runtimeMetricsKeys[i].Type {
					case MetricTypeGauge:
						meter.Gauge(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
					case MetricTypeCounter:
						meter.Count(runtimeMetricsKeys[i].Key, int64(value), WithUnit(runtimeMetricsKeys[i].Unit))
					case MetricTypeDistribution:
						meter.Distribution(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
					}
				case runtime_metrics.KindFloat64Histogram:
					hist := sample.Value.Float64Histogram()
					if hist == nil || len(hist.Buckets) < 2 || len(hist.Counts) != len(hist.Buckets)-1 {
						// Invalid histogram
						continue
					}

					for j := 0; j < len(hist.Counts); j++ {
						count := hist.Counts[j]

						// Get bucket boundaries
						lowerBound := hist.Buckets[j]
						upperBound := hist.Buckets[j+1]

						// Use the midpoint of the bucket as the representative value
						var value float64
						if math.IsInf(lowerBound, -1) && math.IsInf(upperBound, 1) {
							value = 0 // or some default
						} else if math.IsInf(upperBound, 1) {
							value = lowerBound // For the last bucket, use lower bound
						} else if math.IsInf(lowerBound, -1) {
							value = upperBound
						} else {
							value = (lowerBound + upperBound) / 2 // Midpoint
						}

						// Record each count as a separate sample
						for j := uint64(0); j < count; j++ {
							meter.Distribution(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
						}
					}
				case runtime_metrics.KindBad:
					// not supported on this Go version/platform
					fallthrough
				default:
					continue
				}
			}
		}
	}
}
