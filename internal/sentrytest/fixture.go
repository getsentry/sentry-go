// Package sentrytest provides test fixtures for the Sentry Go SDK.
//
// Fixture bundles a Scope, Client, and MockTransport with assertion helpers,
// eliminating boilerplate across integration and unit tests.
//
// Isolated mode (default) creates an independent scope safe for parallel tests.
// Global mode (WithGlobal) calls sentry.Init for middleware tests that resolve
// the global client.
package sentrytest

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
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
}

// WithGlobal makes the fixture call [sentry.Init] to set the global client.
// Use this for middleware tests that begin from a background context.
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
// It bundles a Scope, Client, and MockTransport with assertion helpers.
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
	cfg.opts.Transport = transport

	if cfg.opts.Dsn == "" {
		cfg.opts.Dsn = testDsn
	}

	f := &Fixture{
		T:           t,
		Transport:   transport,
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
	f.Context, f.Scope = sentry.WithIsolationScope(context.Background())
	f.Context = sentry.ContextWithClient(f.Context, f.Client)

	// Ensure background goroutines (batch processors) are stopped when the test finishes.
	// This is required for synctest bubbles which will panic if blocked goroutines remain
	// after the bubble's root goroutine exits.
	t.Cleanup(func() { f.Client.Close() })

	return f
}

// Flush flushes the fixture's client.
//
// Inside a [Run] bubble it calls [synctest.Wait] first to let all background
// goroutines settle, then flushes under fake time (completing instantly).
// Outside a bubble it awaits for a real Flush.
func (f *Fixture) Flush() {
	f.T.Helper()
	if f.useSynctest {
		synctest.Wait()
	}
	if ok := f.Client.Flush(testutils.FlushTimeout()); !ok {
		f.T.Error("Fixture: client flush timed out")
	}
}

// NewContext returns a derived context with an isolated scope using the
// fixture's client. If parent is nil, [context.Background] is used.
func (f *Fixture) NewContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	ctx, _ := sentry.WithIsolationScope(parent)
	return sentry.ContextWithClient(ctx, f.Client)
}

// Events returns all captured events, including transactions.
//
// TODO: Add typed helper views (errors, transactions, logs, metrics,
// check-ins) when the telemetry processor path is enabled in tests.
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

// AssertScopeIsolation verifies that requestScope is independent from the
// fixture's scope.
func (f *Fixture) AssertScopeIsolation(requestScope *sentry.Scope) {
	f.T.Helper()

	if requestScope == nil {
		f.T.Error("request scope is nil")
		return
	}
	if requestScope == f.Scope {
		f.T.Error("request scope is the same instance as the fixture scope")
		return
	}

	const sentinel = "_sentrytest_isolation_probe"
	f.Scope.SetTag(sentinel, "leaked")
	defer f.Scope.RemoveTag(sentinel)

	transport := &sentry.MockTransport{}
	probeClient, err := sentry.NewClient(sentry.ClientOptions{Transport: transport})
	if err != nil {
		f.T.Errorf("create probe client: %v", err)
		return
	}
	defer probeClient.Close()
	ctx := sentry.ContextWithClient(
		sentry.ContextWithScope(context.Background(), requestScope),
		probeClient,
	)
	if eventID := sentry.CaptureMessage(ctx, "scope isolation probe"); eventID == nil {
		f.T.Error("scope isolation probe was dropped")
		return
	}

	events := transport.Events()
	if len(events) == 0 {
		f.T.Error("probe event was not delivered")
		return
	}
	if _, ok := events[len(events)-1].Tags[sentinel]; ok {
		f.T.Error("scope mutation leaked into request scope; scopes are not independent")
	}
}
