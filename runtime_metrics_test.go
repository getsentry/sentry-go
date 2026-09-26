package sentry

import (
	"bytes"
	"context"
	"io"
	runtime_metrics "runtime/metrics"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeMetricsWaitTimeout bounds every wait these tests do on the collector
// goroutine. Every wait covers a hand-off that resolves immediately when the
// integration behaves, so the timeout only ever fires on a regression.
const runtimeMetricsWaitTimeout = 5 * time.Second

// runtimeMetricsHarness wires an isolated client (MockTransport) to a
// cancellable context, and swaps the collector's ticker for a channel the test
// drives by hand.
type runtimeMetricsHarness struct {
	t         *testing.T
	ctx       context.Context
	cancel    context.CancelFunc
	transport *MockTransport
	client    *Client

	tickCh    chan time.Time // stands in for time.Ticker.C
	started   chan struct{}  // closed once the collector asks for its ticker
	done      chan struct{}  // closed once the collector returns
	startOnce sync.Once
	launched  bool          // collector goroutine was started by this harness
	interval  time.Duration // interval the collector asked for
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
	return &runtimeMetricsHarness{
		t:         t,
		ctx:       ctx,
		cancel:    cancel,
		transport: transport,
		client:    client,
		tickCh:    make(chan time.Time),
		started:   make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// newTicker is the collector's injected ticker factory. It records the
// requested interval and hands back the harness' tick channel; the stop
// function is a no-op because the harness owns the channel.
func (h *runtimeMetricsHarness) newTicker(d time.Duration) (<-chan time.Time, func()) {
	h.interval = d
	h.startOnce.Do(func() { close(h.started) })
	return h.tickCh, func() {}
}

// launch runs the collector in the background on the harness' fake ticker and
// waits until it has asked for that ticker, so the package-level key/sample
// slices are already updated and the requested interval recorded when launch
// returns.
func (h *runtimeMetricsHarness) launch(config RuntimeMetricsConfig) {
	h.t.Helper()
	config.newTicker = h.newTicker
	h.launched = true
	go func() {
		defer close(h.done)
		StartRuntimeMetrics(config)
	}()
	select {
	case <-h.started:
	case <-time.After(runtimeMetricsWaitTimeout):
		h.t.Fatal("collector never asked for a ticker")
	}
}

// start launches the collector cancelled by the harness context.
func (h *runtimeMetricsHarness) start(config RuntimeMetricsConfig) {
	h.t.Helper()
	config.Context = h.ctx
	h.launch(config)
	h.t.Cleanup(func() {
		runtimeMetricsMutex.Lock()
		runtimeMetricsRunning.Store(false)
		runtimeMetricsMutex.Unlock()
	})
}

// assertReturnsImmediately runs the integration in the background and fails if
// it does not return straight away. Running it in a goroutine (rather than
// synchronously) keeps a regression that drops a start guard from hanging the
// test on its first tick instead of failing.
func (h *runtimeMetricsHarness) assertReturnsImmediately(config RuntimeMetricsConfig) {
	h.t.Helper()
	config.Context = h.ctx
	config.newTicker = h.newTicker
	returned := make(chan struct{})
	go func() {
		StartRuntimeMetrics(config)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(runtimeMetricsWaitTimeout):
		h.t.Fatal("StartRuntimeMetrics did not return immediately")
	}
}

// tick hands the collector one tick. It returns once the tick has been
// received, which means every earlier tick was fully processed: the collector
// can only receive the next tick after it finished the previous iteration.
func (h *runtimeMetricsHarness) tick() {
	h.t.Helper()
	select {
	case h.tickCh <- time.Now():
	case <-time.After(runtimeMetricsWaitTimeout):
		h.t.Fatal("collector did not consume a tick")
	}
}

// waitForCollector waits for the collector goroutine to return, so no test
// body observes state the collector could still be writing.
func (h *runtimeMetricsHarness) waitForCollector() {
	h.t.Helper()
	select {
	case <-h.done:
	case <-time.After(runtimeMetricsWaitTimeout):
		h.t.Fatal("collector did not stop")
	}
}

// stopCollecting cancels the context and waits for the collector goroutine to
// exit. Cancelling alone is harmless when no collector was launched.
func (h *runtimeMetricsHarness) stopCollecting() {
	h.t.Helper()
	h.cancel()
	if h.launched {
		h.waitForCollector()
	}
}

func (h *runtimeMetricsHarness) flush() {
	h.t.Helper()
	flushFromContext(h.ctx, testutils.FlushTimeout())
}

// shutdown stops the client's batch processors.
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

// assertCollectorConsumesNoTicks fails if anything is still reading the
// harness' tick channel, i.e. if a collector outlived the test's attempt to
// stop it. It waits out a short real window so a collector that is simply not
// parked yet still gets the chance to pick the tick up.
func (h *runtimeMetricsHarness) assertCollectorConsumesNoTicks() {
	h.t.Helper()
	select {
	case h.tickCh <- time.Now():
		h.t.Fatal("collector consumed a tick after it should have stopped")
	case <-time.After(50 * time.Millisecond):
	}
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
	clock := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	tracker := &cpuUtilizationTracker{now: func() time.Time { return clock }}
	require.True(t, tracker.lastSampleTime.IsZero())

	// First sample cannot be computed.
	got := tracker.GetCPUUtilization(2)
	assert.Equal(t, 0.0, got)
	assert.Equal(t, 2.0, tracker.lastCPUSeconds)
	assert.False(t, tracker.lastSampleTime.IsZero())

	// Half of GOMAXPROCS consumed over exactly one second of wall clock.
	clock = clock.Add(time.Second)
	prev := tracker.lastCPUSeconds
	got = tracker.GetCPUUtilization(prev + 0.5*maxProcs)
	assert.InDelta(t, 0.5, got, 1e-9)
	assert.Equal(t, prev+0.5*maxProcs, tracker.lastCPUSeconds)

	// Clamped to 1.0 when the CPU-seconds delta exceeds wall clock.
	clock = clock.Add(time.Second)
	prev = tracker.lastCPUSeconds
	got = tracker.GetCPUUtilization(prev + 2*maxProcs)
	assert.Equal(t, 1.0, got)
	assert.Equal(t, prev+2*maxProcs, tracker.lastCPUSeconds)

	// Clamped to 0.0 when the counter goes backwards (counter reset,
	// process restart, jitter).
	clock = clock.Add(time.Second)
	got = tracker.GetCPUUtilization(0)
	assert.Equal(t, 0.0, got)
	assert.Equal(t, 0.0, tracker.lastCPUSeconds)

	// No progress at all between samples.
	clock = clock.Add(time.Second)
	got = tracker.GetCPUUtilization(0)
	assert.Equal(t, 0.0, got)
}

func Test_StartRuntimeMetrics_Disabled(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()
	defer h.stopCollecting()

	require.False(t, runtimeMetricsRunning.Load())

	// Disabled must return immediately, before the running guard is set.
	h.assertReturnsImmediately(RuntimeMetricsConfig{Disabled: true, Interval: time.Second})

	h.flush()

	assert.Empty(t, h.transport.Events())
	assert.False(t, runtimeMetricsRunning.Load())
}

func Test_StartRuntimeMetrics_AlreadyRunning(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()

	runtimeMetricsMutex.Lock()
	runtimeMetricsRunning.Store(true)
	runtimeMetricsMutex.Unlock()
	defer func() {
		runtimeMetricsMutex.Lock()
		runtimeMetricsRunning.Store(false)
		runtimeMetricsMutex.Unlock()
	}()
	// Cancelling is a no-op for the passing case; it releases a collector that
	// a regression started despite the running guard.
	defer h.stopCollecting()

	// Returns at the running guard instead of starting a second collector.
	h.assertReturnsImmediately(RuntimeMetricsConfig{Interval: time.Second})

	h.flush()

	assert.Empty(t, h.transport.Events())
}

func Test_StartRuntimeMetrics_DefaultInterval(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()
	defer h.stopCollecting()

	h.start(RuntimeMetricsConfig{})

	// Interval unset must fall back to the documented 30s.
	assert.Equal(t, 30*time.Second, h.interval)

	// Nothing is emitted before the first tick.
	h.flush()
	assert.Empty(t, h.transport.Events())

	h.tick()
	h.stopCollecting()
	h.flush()

	events := h.transport.Events()
	require.Len(t, events, 1)
	assert.Len(t, events[0].Metrics, len(runtimeMetricsKeys))
	assertMetricsAreWellFormed(t, events[0].Metrics)
}

func Test_StartRuntimeMetrics_CollectsDeclaredMetrics(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()
	defer h.stopCollecting()

	h.start(RuntimeMetricsConfig{Interval: time.Second})

	// Three ticks; each one is only delivered after the previous was fully
	// processed, so all three are collected before the collector is stopped.
	h.tick()
	h.tick()
	h.tick()
	h.stopCollecting()
	h.flush()

	events := h.transport.Events()
	require.Len(t, events, 1)
	assert.Equal(t, traceMetricEvent.Type, events[0].Type)

	// Every declared key is emitted once per interval.
	require.Len(t, events[0].Metrics, 3*len(runtimeMetricsKeys))
	assertMetricsAreWellFormed(t, events[0].Metrics)
}

func Test_StartRuntimeMetrics_StopsOnContextCancel(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()
	defer h.stopCollecting()

	h.start(RuntimeMetricsConfig{Interval: time.Second})

	h.tick()
	h.tick()
	h.stopCollecting()
	h.flush()

	events := h.transport.Events()
	require.Len(t, events, 1)
	want := len(events[0].Metrics)
	require.Equal(t, 2*len(runtimeMetricsKeys), want)

	// The collector goroutine has returned, so it can neither take another tick
	// nor emit another batch.
	h.assertCollectorConsumesNoTicks()
	h.flush()

	total := 0
	for _, event := range h.transport.Events() {
		total += len(event.Metrics)
	}
	assert.Equal(t, want, total)
}

func Test_StartRuntimeMetrics_CollectOptionalMetrics(t *testing.T) {
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

	h.start(RuntimeMetricsConfig{Interval: time.Second, CollectOptionalMetrics: true})

	assert.Len(t, runtimeMetricsKeys, len(origKeys)+2)
	assert.Len(t, runtimeMetricsSamples, len(origSamples)+2)

	h.tick()
	h.stopCollecting()
	h.flush()

	all := h.collectedMetrics()
	assertMetricsAreWellFormed(t, all)

	// One interval of the base set plus at least one opt-in metric, so the
	// opt-in must have contributed to the emission.
	assert.Greater(t, len(all), len(origKeys))
}

func Test_StartRuntimeMetrics_NilContextUsesCurrentHub(t *testing.T) {
	h := newRuntimeMetricsHarness(t, ClientOptions{})
	defer h.shutdown()

	// Without a Context the collector builds one from the current hub, so it
	// can never be cancelled. Panic on the second interval instead: the metric
	// callback runs inside the collector goroutine and its deferred recover
	// is the only way to stop an uncancellable collector.
	//
	// Counting emissions rather than flipping a flag keeps the panic on the
	// second interval: the first interval emits one metric per declared key
	// and the callback is entered concurrently with the test.
	var emitted atomic.Int64
	h.client.options.BeforeSendMetric = func(m *Metric) *Metric {
		if emitted.Add(1) > int64(len(runtimeMetricsKeys)) {
			panic("stop runtime metrics collector")
		}
		return m
	}

	hub := CurrentHub()
	prev := hub.Client()
	hub.BindClient(h.client)
	defer hub.BindClient(prev)
	defer func() {
		runtimeMetricsMutex.Lock()
		runtimeMetricsRunning.Store(false)
		runtimeMetricsMutex.Unlock()
	}()

	h.launch(RuntimeMetricsConfig{Interval: time.Second})

	h.tick()
	h.tick()
	h.waitForCollector()

	h.flush()

	// The collector resolves its hub from CurrentHub; a client bound there
	// is what makes the metrics reachable in the first place.
	events := h.transport.Events()
	require.Len(t, events, 1)
	assert.Len(t, events[0].Metrics, len(runtimeMetricsKeys))
	assertMetricsAreWellFormed(t, events[0].Metrics)
}

func Test_StartRuntimeMetrics_RecoversPanicInEmit(t *testing.T) {
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
	h.tick()
	h.stopCollecting()
	h.flush()

	assert.Empty(t, h.transport.Events())
	assert.Contains(t, buf.String(), "panic during runtime metrics integration")
	assert.Contains(t, buf.String(), "boom")
}
