package sentry

import (
	"bytes"
	"context"
	"io"
	runtime_metrics "runtime/metrics"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeMetricsHarness wires an isolated client (MockTransport) to a
// cancellable context. It must be constructed inside a synctest bubble: the
// client's batch processor goroutines are part of that bubble and are stopped
// by shutdown.
type runtimeMetricsHarness struct {
	t         *testing.T
	ctx       context.Context
	cancel    context.CancelFunc
	transport *MockTransport
	client    *Client
}

func newRuntimeMetricsHarness(t *testing.T, opts ClientOptions) *runtimeMetricsHarness {
	t.Helper()
	transport := &MockTransport{}
	opts.Transport = transport
	if opts.Dsn == "" {
		opts.Dsn = testDsn
	}
	client, err := NewClient(opts)
	require.NoError(t, err)
	hub := CurrentHub().Clone()
	hub.BindClient(client)
	ctx, cancel := context.WithCancel(SetHubOnContext(context.Background(), hub))
	return &runtimeMetricsHarness{t: t, ctx: ctx, cancel: cancel, transport: transport, client: client}
}

// start launches the integration in the background and waits until it is
// parked on its first tick, so the package-level key/sample slices are already
// updated when start returns.
func (h *runtimeMetricsHarness) start(config RuntimeMetricsConfig) {
	h.t.Helper()
	config.Context = h.ctx
	go StartRuntimeMetrics(config)
	synctest.Wait()
	h.t.Cleanup(func() { runtimeMetricsRunning = false })
}

// assertReturnsImmediately runs the integration in the background and fails if
// it does not return straight away. Running it in a goroutine (rather than
// synchronously) keeps a regression that drops a start guard from hijacking
// the bubble's root goroutine, which would livelock instead of failing.
func (h *runtimeMetricsHarness) assertReturnsImmediately(config RuntimeMetricsConfig) {
	h.t.Helper()
	config.Context = h.ctx
	returned := make(chan struct{})
	go func() {
		StartRuntimeMetrics(config)
		close(returned)
	}()
	// Wait lets the goroutine either exit or park on its first tick.
	synctest.Wait()
	select {
	case <-returned:
	default:
		h.t.Fatal("StartRuntimeMetrics did not return immediately")
	}
}

// stopCollecting cancels the context and waits for the collector goroutine to
// exit, so no goroutine reads the package-level slices after the test body.
func (h *runtimeMetricsHarness) stopCollecting() {
	h.t.Helper()
	h.cancel()
	synctest.Wait()
}

func (h *runtimeMetricsHarness) flush() {
	h.t.Helper()
	synctest.Wait()
	flushFromContext(h.ctx, testutils.FlushTimeout())
}

// shutdown stops the client's batch processors. Required before a bubble ends,
// otherwise the bubble reports a deadlock.
func (h *runtimeMetricsHarness) shutdown() {
	h.t.Helper()
	h.client.Close()
}

// collectedMetrics returns every metric from every event the transport saw.
func (h *runtimeMetricsHarness) collectedMetrics() []Metric {
	h.t.Helper()
	var all []Metric
	for _, event := range h.transport.Events() {
		all = append(all, event.Metrics...)
	}
	return all
}

// assertMetricsAreWellFormed checks that the collector emitted usable metrics:
// each one named, typed, and carrying a value of the kind its type promises.
// Deliberately does not pin names, units or magnitudes.
func assertMetricsAreWellFormed(t *testing.T, metrics []Metric) {
	t.Helper()
	for _, m := range metrics {
		assert.NotEmpty(t, m.Name, "emitted metric without a name")
		assert.NotEqual(t, MetricTypeInvalid, m.Type, "metric %q emitted without a type", m.Name)

		_, isInt := m.Value.Int64()
		_, isFloat := m.Value.Float64()
		assert.True(t, isInt || isFloat, "metric %q emitted without a value", m.Name)

		switch m.Type {
		case MetricTypeCounter:
			assert.True(t, isInt, "counter %q should carry an int64", m.Name)
		default:
			assert.True(t, isFloat, "%s %q should carry a float64", m.Type, m.Name)
		}
	}
}

func Test_runtimeMetricsKeysAndSamplesAreInSync(t *testing.T) {
	// The collector walks the samples and indexes the keys with the same index,
	// so a key without its sample (or vice versa) must never get past this.
	require.Len(t, runtimeMetricsSamples, len(runtimeMetricsKeys))
}

func Test_runtimeMetricsSamplesAreReadable(t *testing.T) {
	// Read mutates its argument, so hand it a copy: the production code reads
	// the shared package-level slice directly.
	samples := make([]runtime_metrics.Sample, len(runtimeMetricsSamples))
	copy(samples, runtimeMetricsSamples)

	runtime_metrics.Read(samples)

	for i, sample := range samples {
		assert.NotEqual(t, runtime_metrics.KindBad, sample.Value.Kind(),
			"sample %d (%q) is not supported by this Go version", i, sample.Name)
	}
}

func Test_cpuUtilizationTracker_GetCPUUtilization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := &cpuUtilizationTracker{}
		require.True(t, tracker.lastSampleTime.IsZero())

		// First sample cannot be computed.
		got := tracker.GetCPUUtilization(2)
		assert.Equal(t, 0.0, got)
		assert.Equal(t, 2.0, tracker.lastCPUSeconds)
		assert.False(t, tracker.lastSampleTime.IsZero())

		// Half of GOMAXPROCS consumed over exactly one second of wall clock.
		synctest.Sleep(time.Second)
		prev := tracker.lastCPUSeconds
		got = tracker.GetCPUUtilization(prev + 0.5*maxProcs)
		assert.InDelta(t, 0.5, got, 1e-9)
		assert.Equal(t, prev+0.5*maxProcs, tracker.lastCPUSeconds)

		// Clamped to 1.0 when the CPU-seconds delta exceeds wall clock.
		synctest.Sleep(time.Second)
		prev = tracker.lastCPUSeconds
		got = tracker.GetCPUUtilization(prev + 2*maxProcs)
		assert.Equal(t, 1.0, got)
		assert.Equal(t, prev+2*maxProcs, tracker.lastCPUSeconds)

		// Clamped to 0.0 when the counter goes backwards (counter reset,
		// process restart, jitter).
		synctest.Sleep(time.Second)
		got = tracker.GetCPUUtilization(0)
		assert.Equal(t, 0.0, got)
		assert.Equal(t, 0.0, tracker.lastCPUSeconds)

		// No progress at all between samples.
		synctest.Sleep(time.Second)
		got = tracker.GetCPUUtilization(0)
		assert.Equal(t, 0.0, got)
	})
}

func Test_StartRuntimeMetrics_Disabled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		require.False(t, runtimeMetricsRunning)

		// Disabled must return immediately, before the running guard is set.
		h.assertReturnsImmediately(RuntimeMetricsConfig{Disabled: true, Interval: time.Second})

		synctest.Sleep(3 * time.Second)
		h.flush()

		assert.Empty(t, h.transport.Events())
		assert.False(t, runtimeMetricsRunning)
	})
}

func Test_StartRuntimeMetrics_AlreadyRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()

		runtimeMetricsRunning = true
		defer func() { runtimeMetricsRunning = false }()
		// Cancelling is a no-op for the passing case; it keeps a regression that
		// drops the running guard from leaking a collector into the bubble.
		defer h.stopCollecting()

		// Returns at the running guard instead of starting a second collector.
		h.assertReturnsImmediately(RuntimeMetricsConfig{Interval: time.Second})

		synctest.Sleep(3 * time.Second)
		h.flush()

		assert.Empty(t, h.transport.Events())
	})
}

func Test_StartRuntimeMetrics_DefaultInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		h.start(RuntimeMetricsConfig{})

		// Interval unset must fall back to the documented 30s.
		synctest.Sleep(5 * time.Second)
		h.flush()
		assert.Empty(t, h.transport.Events())

		synctest.Sleep(26 * time.Second)
		h.stopCollecting()
		h.flush()

		events := h.transport.Events()
		require.Len(t, events, 1)
		assert.Len(t, events[0].Metrics, len(runtimeMetricsKeys))
		assertMetricsAreWellFormed(t, events[0].Metrics)
	})
}

func Test_StartRuntimeMetrics_CollectsDeclaredMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		h.start(RuntimeMetricsConfig{Interval: time.Second})

		// Three ticks; the extra millisecond guarantees the tick at t=3s has
		// been fully processed before the collector is stopped.
		synctest.Sleep(3*time.Second + time.Millisecond)
		h.stopCollecting()
		h.flush()

		events := h.transport.Events()
		require.Len(t, events, 1)
		assert.Equal(t, traceMetricEvent.Type, events[0].Type)

		// Every declared key is emitted once per interval.
		require.Len(t, events[0].Metrics, 3*len(runtimeMetricsKeys))
		assertMetricsAreWellFormed(t, events[0].Metrics)
	})
}

func Test_StartRuntimeMetrics_StopsOnContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		h.start(RuntimeMetricsConfig{Interval: time.Second})

		synctest.Sleep(2*time.Second + time.Millisecond)
		h.stopCollecting()
		h.flush()

		events := h.transport.Events()
		require.Len(t, events, 1)
		want := len(events[0].Metrics)
		require.Equal(t, 2*len(runtimeMetricsKeys), want)

		// Ten more intervals; a collector that ignored ctx.Done() would emit ten
		// more batches here.
		synctest.Sleep(10 * time.Second)
		h.flush()

		total := 0
		for _, event := range h.transport.Events() {
			total += len(event.Metrics)
		}
		assert.Equal(t, want, total)
	})
}

func Test_StartRuntimeMetrics_CollectGCMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The opt-in appends to the package-level tables; restore them so later
		// tests collect the base set only.
		origKeys, origSamples := runtimeMetricsKeys, runtimeMetricsSamples
		defer func() {
			runtimeMetricsKeys = origKeys
			runtimeMetricsSamples = origSamples
		}()

		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		h.start(RuntimeMetricsConfig{Interval: time.Second, CollectGCMetrics: true})

		assert.Len(t, runtimeMetricsKeys, len(origKeys)+2)
		assert.Len(t, runtimeMetricsSamples, len(origSamples)+2)

		synctest.Sleep(time.Second + time.Millisecond)
		h.stopCollecting()
		h.flush()

		all := h.collectedMetrics()
		assertMetricsAreWellFormed(t, all)

		// One interval of the base set plus at least one opt-in metric, so the
		// opt-in must have contributed to the emission.
		assert.Greater(t, len(all), len(origKeys))
	})
}

func Test_StartRuntimeMetrics_NilContextUsesCurrentHub(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()

		// Without a Context the collector builds one from the current hub, so it
		// can never be cancelled. Panic on the second tick instead: the metric
		// callback runs inside the collector goroutine and its deferred recover
		// is the only way to stop an uncancellable collector.
		var stop atomic.Bool
		h.client.options.BeforeSendMetric = func(m *Metric) *Metric {
			if stop.Load() {
				panic("stop runtime metrics collector")
			}
			return m
		}

		hub := CurrentHub()
		prev := hub.Client()
		hub.BindClient(h.client)
		defer hub.BindClient(prev)
		defer func() { runtimeMetricsRunning = false }()

		go StartRuntimeMetrics(RuntimeMetricsConfig{Interval: time.Second})
		synctest.Wait()

		synctest.Sleep(time.Second + time.Millisecond)
		stop.Store(true)
		synctest.Sleep(2 * time.Second)

		h.flush()

		// The collector resolves its hub from CurrentHub; a client bound there
		// is what makes the metrics reachable in the first place.
		events := h.transport.Events()
		require.Len(t, events, 1)
		assert.Len(t, events[0].Metrics, len(runtimeMetricsKeys))
		assertMetricsAreWellFormed(t, events[0].Metrics)
	})
}

func Test_StartRuntimeMetrics_RecoversPanicInEmit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		debuglog.SetOutput(&buf)
		defer debuglog.SetOutput(io.Discard)

		h := newRuntimeMetricsHarness(t, ClientOptions{})
		defer h.shutdown()
		defer h.stopCollecting()

		h.client.options.BeforeSendMetric = func(*Metric) *Metric {
			panic("boom")
		}

		h.start(RuntimeMetricsConfig{Interval: time.Second})

		// The first tick panics on its first metric and unwinds the collector
		// goroutine, so no later tick can emit anything.
		synctest.Sleep(5*time.Second + time.Millisecond)

		h.stopCollecting()
		h.flush()

		assert.Empty(t, h.transport.Events())
		assert.Contains(t, buf.String(), "panic during runtime metrics integration")
		assert.Contains(t, buf.String(), "boom")
	})
}
