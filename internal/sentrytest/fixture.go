// Package sentrytest provides test fixtures for the Sentry Go SDK.
//
// Fixture bundles a context, scope, client, and MockTransport with assertion helpers,
// eliminating boilerplate across integration and unit tests.
//
// Isolated mode (default) creates a context and scope safe for parallel tests.
// Global mode (WithGlobal) calls sentry.Init for tests that use global state.
package sentrytest

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const testDsn = "https://whatever@sentry.io/1337"

// DefaultEventCmpOpts are [cmp.Options] for comparing [sentry.Event] values.
// They ignore fields that vary between runs (IDs, timestamps, server metadata)
// and all unexported fields.
var DefaultEventCmpOpts = cmp.Options{
	cmpopts.IgnoreFields(sentry.Event{},
		"Contexts",
		"EventID",
		"Modules",
		"Platform",
		"Release",
		"Sdk",
		"ServerName",
		"Timestamp",
	),
	cmpopts.IgnoreFields(sentry.Request{}, "Env"),
	cmpopts.IgnoreUnexported(sentry.Event{}),
	cmpopts.EquateEmpty(),
}

// Option configures a [Fixture].
type Option func(*config)

type config struct {
	opts   sentry.ClientOptions
	global bool
	held   bool
}

// WithHeldDelivery keeps captured telemetry buffered and unencoded until
// [Fixture.Flush], as it is while the background worker is busy.
func WithHeldDelivery() Option {
	return func(c *config) {
		c.held = true
	}
}

// recorder can hold the delivery of envelopes to the transport.
type recorder struct {
	*sentry.MockTransport

	hold    chan struct{}
	holding chan struct{}
	holdOne sync.Once
	release sync.Once
}

func (r *recorder) Configure(options sentry.ClientOptions) {
	if r.hold == nil {
		r.MockTransport.Configure(options)
	}
}

func (r *recorder) SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	if r.hold != nil {
		held := false
		r.holdOne.Do(func() {
			held = true
			close(r.holding)
			<-r.hold
		})
		if held {
			return nil
		}
	}
	return r.MockTransport.SendEnvelope(ctx, envelope)
}

func (r *recorder) stopHolding() {
	if r.hold != nil {
		r.release.Do(func() { close(r.hold) })
	}
}

// WithGlobal makes the fixture call [sentry.Init] to set the global client
// instead of creating an isolated client context.
//
// Tests using global mode must not run in parallel.
func WithGlobal() Option {
	return func(c *config) {
		c.global = true
	}
}

// WithClientOptions sets the [sentry.ClientOptions] for the fixture.
// Transport is always overridden to use the fixture's mock transport. If Dsn is
// empty, the fixture sets a placeholder test DSN.
func WithClientOptions(opts sentry.ClientOptions) Option {
	return func(c *config) {
		c.opts = opts
	}
}

// Fixture provides an isolated Sentry environment for testing.
// It bundles a context, scope, client, and MockTransport with assertion helpers.
type Fixture struct {
	// T is the test context.
	T testing.TB

	// Context carries the fixture's isolated scope and client.
	Context context.Context

	// Scope is the fixture's isolated scope.
	Scope *sentry.Scope

	// Client is the fixture's client, configured with the MockTransport.
	Client *sentry.Client

	// Transport captures all events sent through the client.
	Transport *sentry.MockTransport

	recorder *recorder

	// useSynctest indicates the fixture is running inside a synctest bubble.
	useSynctest bool
}

// Run creates a [Fixture] inside a [synctest.Test] bubble and calls fn
// with it. All background goroutines (batch processors) use fake time, so
// [Fixture.Flush] completes instantly. This is the preferred way to
// create fixtures.
//
// For the rare case where synctest is incompatible (e.g. third-party code that
// leaks goroutines), use [NewFixture] directly.
func Run(t *testing.T, fn func(t *testing.T, f *Fixture), opts ...Option) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t, true, opts...)
		fn(t, f)
	})
}

// NewFixture creates a new test fixture without a synctest bubble.
// Prefer [Run] for most tests; use this only when synctest is incompatible.
func NewFixture(t testing.TB, opts ...Option) *Fixture {
	t.Helper()
	return newFixture(t, false, opts...)
}

// NewContext creates a context backed by a new [Fixture] without a synctest
// bubble. If parent is nil, [context.Background] is used.
func NewContext(parent context.Context, t testing.TB, opts ...Option) context.Context {
	t.Helper()
	return NewFixture(t, opts...).NewContext(parent)
}

func newFixture(t testing.TB, useSynctest bool, opts ...Option) *Fixture {
	t.Helper()

	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	transport := &sentry.MockTransport{}
	rec := &recorder{MockTransport: transport}
	if cfg.held {
		rec.hold = make(chan struct{})
		rec.holding = make(chan struct{})
	}
	cfg.opts.Transport = rec

	if cfg.opts.Dsn == "" {
		cfg.opts.Dsn = testDsn
	}

	f := &Fixture{
		T:           t,
		Transport:   transport,
		recorder:    rec,
		useSynctest: useSynctest,
	}

	if cfg.global {
		if err := sentry.Init(cfg.opts); err != nil {
			t.Fatal(err)
		}
		f.Client = sentry.ClientFromContext(context.Background())
	} else {
		client, err := sentry.NewClient(cfg.opts)
		if err != nil {
			t.Fatal(err)
		}
		f.Client = client
	}
	f.Context, f.Scope = sentry.WithScope(context.Background())
	f.Context = sentry.ContextWithClient(f.Context, f.Client)

	// Ensure background goroutines (batch processors) are stopped when the test finishes.
	// This is required for synctest bubbles which will panic if blocked goroutines remain
	// after the bubble's root goroutine exits.
	t.Cleanup(func() {
		rec.stopHolding()
		f.Client.Close()
	})

	if cfg.held {
		// The worker sends one envelope at a time, so this one keeps the rest buffered.
		f.Client.CaptureCheckIn(
			sentry.ContextWithClient(context.Background(), f.Client),
			&sentry.CheckIn{MonitorSlug: "sentrytest-held", Status: sentry.CheckInStatusOK},
			nil,
		)
		<-rec.holding
	}

	return f
}

// Flush flushes the fixture's client.
//
// Inside a [Run] bubble it calls [synctest.Wait] first to let all background
// goroutines settle, then flushes under fake time (completing instantly).
// Outside a bubble it awaits for a real Flush.
func (f *Fixture) Flush() {
	f.T.Helper()
	f.recorder.stopHolding()
	if f.useSynctest {
		synctest.Wait()
	}
	if ok := f.Client.Flush(testutils.FlushTimeout()); !ok {
		f.T.Error("Fixture: client flush timed out")
	}
}

// NewContext returns parent with a fresh fixture scope and client. If parent is
// nil, [context.Background] is used.
func (f *Fixture) NewContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	ctx, _ := sentry.WithScope(parent)
	return sentry.ContextWithClient(ctx, f.Client)
}

// Envelopes returns the envelopes handed to the transport so far.
func (f *Fixture) Envelopes() []*protocol.Envelope {
	return f.Transport.Envelopes()
}

// Events returns all captured events, including transactions.
//
// TODO: Add typed helper views (errors, transactions, logs, metrics,
// check-ins).
func (f *Fixture) Events() []*sentry.Event {
	return f.Transport.Events()
}

// AssertEventCount flushes and asserts the number of captured events.
func (f *Fixture) AssertEventCount(want int) {
	f.T.Helper()
	f.assertCount("event", want)
}

func (f *Fixture) assertCount(kind string, want int) {
	f.T.Helper()
	f.Flush()
	if got := len(f.Events()); got != want {
		f.T.Errorf("%s count: got %d, want %d", kind, got, want)
	}
}

// DiffEvents flushes and returns a [cmp.Diff] of captured events
// against want. Uses [DefaultEventCmpOpts] merged with any additional opts.
// Returns "" when events match.
func (f *Fixture) DiffEvents(want []*sentry.Event, opts ...cmp.Option) string {
	f.T.Helper()
	f.Flush()
	combined := make(cmp.Options, 0, len(DefaultEventCmpOpts)+len(opts))
	combined = append(combined, DefaultEventCmpOpts...)
	for _, o := range opts {
		combined = append(combined, o)
	}
	return cmp.Diff(want, f.Events(), combined...)
}
