package sentry

import (
	"context"
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
}

// The following metrics are collected by the runtime metrics integration.
// This should be synced with the `runtimeMetricsSamples` variable.
var runtimeMetricsKeys = []struct {
	Key  string
	Unit string
}{
	{"go.runtime.mem.total", UnitByte},
	{"go.runtime.mem.heap_objects", UnitByte},
	{"go.runtime.mem.heap_free", UnitByte},
	{"go.runtime.mem.heap_unused", UnitByte},
	{"go.runtime.mem.other", UnitByte},
	{"go.runtime.goroutines.total", "goroutines"},
	{"go.runtime.cpu.utilization", UnitRatio},
}

// The following metrics are collected by the runtime metrics integration.
// This should be synced with the `runtimeMetricsKeys` variable.
var runtimeMetricsSamples = []runtime_metrics.Sample{
	{Name: "/memory/classes/total:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/heap/free:bytes"},
	{Name: "/memory/classes/heap/unused:bytes"},
	{Name: "/memory/classes/other:bytes"},
	{Name: "/sched/goroutines:goroutines"},
	{Name: "/cpu/classes/total:cpu-seconds"},
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
}

var maxProcs = float64(runtime.GOMAXPROCS(-1))

func (t *cpuUtilizationTracker) GetCPUUtilization(currentCPUSeconds float64) float64 {
	now := time.Now()

	// First sample — can't calculate yet
	if t.lastSampleTime.IsZero() {
		t.lastCPUSeconds = currentCPUSeconds
		t.lastSampleTime = now
		return 0.0
	}

	// Calculate deltas
	cpuDelta := currentCPUSeconds - t.lastCPUSeconds
	wallClockDelta := now.Sub(t.lastSampleTime).Seconds()

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
	t.lastSampleTime = now

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

	timer := time.NewTicker(config.Interval)
	defer timer.Stop()
	cpuTracker := cpuUtilizationTracker{}

	for {
		select {
		case <-config.Context.Done():
			return
		case <-timer.C:
			runtime_metrics.Read(runtimeMetricsSamples)
			for i, sample := range runtimeMetricsSamples {
				switch sample.Value.Kind() {
				case runtime_metrics.KindFloat64:
					value := sample.Value.Float64()
					if i == 6 {
						value = cpuTracker.GetCPUUtilization(value)
					}
					meter.Gauge(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
				case runtime_metrics.KindUint64:
					value := float64(sample.Value.Uint64())
					if i == 6 {
						value = cpuTracker.GetCPUUtilization(value)
					}
					meter.Gauge(runtimeMetricsKeys[i].Key, value, WithUnit(runtimeMetricsKeys[i].Unit))
				case runtime_metrics.KindFloat64Histogram:
					// XXX(aldy505): we don't have any. see for metric samples
					// that has "Distribution" on the description.
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
