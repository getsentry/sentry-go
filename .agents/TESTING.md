# Testing Guidelines

## Philosophy

Prefer tests that exercise real integration behavior over isolated unit tests.
The best tests call the same APIs that users call — `sentry.Init`, `CaptureException`,
`Flush`, `StartSpan`, and the HTTP middleware — rather than mocking internal components.

A test that sends an HTTP request through a real `httptest.Server`, triggers a panic
recovery in middleware, and asserts on the captured `*sentry.Event` is far more
valuable than one that calls an unexported helper in isolation.

## Test Tiers

Use the highest tier that covers what you need to verify.

### 1. Integration Tests (preferred)

Use `sentry.Init` with `BeforeSend` / `BeforeSendTransaction` callbacks to capture
events through the full pipeline. For HTTP integrations, spin up an `httptest.Server`
with the real framework and make actual requests.

```go
func TestCapturesPanic(t *testing.T) {
    eventsCh := make(chan *sentry.Event, 1)

    err := sentry.Init(sentry.ClientOptions{
        EnableTracing:    true,
        TracesSampleRate: 1.0,
        BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
            eventsCh <- event
            return event
        },
    })
    if err != nil {
        t.Fatal(err)
    }

    router := gin.New()
    router.Use(sentrygin.New(sentrygin.Options{Repanic: false}))
    router.GET("/panic", func(c *gin.Context) { panic("boom") })

    srv := httptest.NewServer(router)
    defer srv.Close()

    resp, err := srv.Client().Get(srv.URL + "/panic")
    if err != nil {
        t.Fatal(err)
    }
    resp.Body.Close()

    if ok := sentry.Flush(testutils.FlushTimeout()); !ok {
        t.Fatal("sentry.Flush timed out")
    }
    close(eventsCh)

    event := <-eventsCh
    if event == nil {
        t.Fatal("expected a captured event")
    }
    if event.Exception[0].Value != "boom" {
        t.Errorf("got exception value %q, want %q", event.Exception[0].Value, "boom")
    }
}
```

### 2. Context-Level Tests

When you need a real `Hub` and `Scope` but no HTTP server, use `NewTestContext`
with a `MockTransport`. This is the right level for span/transaction behavior.

```go
func TestChildSpanInheritsTraceID(t *testing.T) {
    transport := &MockTransport{}
    ctx := NewTestContext(ClientOptions{
        EnableTracing:    true,
        TracesSampleRate: 1.0,
        Transport:        transport,
    })

    span := StartSpan(ctx, "parent.op")
    child := span.StartChild("child.op")
    child.Finish()
    span.Finish()

    events := transport.Events()
    if len(events) != 1 {
        t.Fatalf("got %d events, want 1", len(events))
    }
    if events[0].Contexts["trace"]["trace_id"] != span.TraceID.String() {
        t.Error("child span should inherit parent trace ID")
    }
}
```

### 3. Unit Tests (use sparingly)

Reserve direct `NewClient` + `MockTransport` + `MockScope` tests for logic that
is genuinely self-contained — event processors, `BeforeSend` callbacks, sampling
decisions, option parsing. If you find yourself rebuilding half the pipeline in
your test setup, move up a tier.

```go
func TestBeforeSendDropsEvent(t *testing.T) {
    transport := &MockTransport{}
    client, _ := NewClient(ClientOptions{
        Dsn:       "https://key@sentry.io/1",
        Transport: transport,
        BeforeSend: func(event *Event, hint *EventHint) *Event {
            return nil // drop
        },
    })

    client.CaptureMessage("should be dropped", nil, &MockScope{})

    if transport.Events() != nil {
        t.Error("expected event to be dropped")
    }
}
```

## Conventions

- **Table-driven tests** for exercising multiple inputs through the same code path.
- **`t.Parallel()`** for tests that do not share global state.
- **`cmp.Diff`** with `cmpopts.IgnoreFields` for comparing `*sentry.Event` structs —
  ignore dynamic fields like `EventID`, `Timestamp`, `Sdk`, and `sdkMetaData`.
- **`testutils.FlushTimeout()`** when calling `sentry.Flush` — returns a longer
  timeout in CI to avoid flaky failures.
- **`-race` flag** is mandatory. All tests must pass under `make test-race`.

## What to Test

- **Behavior users observe**: Does the middleware capture panics? Does `Flush`
  deliver all buffered events? Does trace propagation set the right headers?
- **Edge cases at system boundaries**: Malformed DSN, nil `Hub`, concurrent
  `CaptureMessage` calls from multiple goroutines, context cancellation mid-flush.
- **Regression cases**: When fixing a bug, add a test that reproduces the
  original failure before applying the fix.

## What Not to Test

- Internal struct field assignments that are already covered by higher-level behavior.
- Unexported helpers that only exist to support a single exported function —
  test the exported function instead.
- Third-party library behavior (e.g., that `gin.Context.Abort` works correctly).

## Thread Safety

The SDK is used concurrently. Any test touching shared state (`Hub`, `Scope`,
global `CurrentHub`) should either:

1. Use `t.Parallel()` with isolated instances, or
2. Explicitly verify safety with goroutines and `sync.WaitGroup`.

```go
func TestConcurrentCapture(t *testing.T) {
    transport := &MockTransport{}
    ctx := NewTestContext(ClientOptions{
        Dsn:       "https://key@sentry.io/1",
        Transport: transport,
    })
    hub := GetHubFromContext(ctx)

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            hub.Clone().CaptureMessage("concurrent")
        }()
    }
    wg.Wait()

    if got := len(transport.Events()); got != 10 {
        t.Errorf("got %d events, want 10", got)
    }
}
```
