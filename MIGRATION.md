# Migration Guide

## Upgrading to v1

Version 1 replaces hubs with context-bound scopes and changes custom transports
to send envelopes, along with quality of life improvements.

Isolation is now the default, where the SDK writes only to the scope carried by the
context you pass, so there is no hub to clone before starting a request. Framework
integrations expose the request's context through `GetContext` and captures take
options such as `WithLevel` instead of separate methods.

### Scopes and contexts

The `Hub` has been removed and gets replaced by `context.Context`. All APIs now require
passing `context.Context` to associate with the correct active trace/operation. Use 
`WithScope` to isolate data for a request, job, or goroutine:

```go
// Before
hub := sentry.CurrentHub().Clone()
hub.ConfigureScope(func(scope *sentry.Scope) {
	scope.SetTag("job", "checkout")
})
hub.CaptureException(err)

// After
ctx, scope := sentry.WithScope(ctx)
scope.SetTag("job", "checkout")
sentry.CaptureException(ctx, err)
```

`WithScope(ctx)` clones the scope attached to `ctx`, or starts an empty scope
when no scope is attached. To propagate the scope the returned context should
be kept.

| Before | v1 |
| --- | --- |
| `CurrentHub().Scope()` or global `ConfigureScope(...)` | `GlobalScope()` for application-wide data |
| `GetHubFromContext(ctx).Scope()` | `ScopeFromContext(ctx)` |
| `HasHubOnContext(ctx)` | `ScopeFromContext(ctx) != nil` to check for an attached scope |
| `hub.Clone()` or callback-based `WithScope(...)` | `ctx, scope := sentry.WithScope(ctx)` |
| `PushScope()` and `PopScope()` | `scopedCtx, scope := sentry.WithScope(ctx)` for the temporary work, then keep using `ctx` |
| `NewHub(client, scope)` and `SetHubOnContext(ctx, hub)` | `ContextWithScope(ctx, scope)` and `ContextWithClient(ctx, client)` |
| `hub.Client()` | `ClientFromContext(ctx)` |
| `hub.BindClient(client)` | `ctx = sentry.ContextWithClient(ctx, client)` |
| `LastEventID()` or `hub.LastEventID()` | `LastEventID(ctx)` |
| `hub.Flush(timeout)` or `hub.FlushWithContext(ctx)` | `ClientFromContext(ctx).Flush(timeout)` or `FlushWithContext(ctx)` |

`HubContextKey` is also removed. Replace direct context-value lookups with
`ScopeFromContext` and `ClientFromContext`. `MockScope` is removed; use
`NewScope()` in tests.

#### Contexts without a scope

A context that carries no scope, including `context.Background()`, has nowhere
to keep state between calls. Events captured with it still receive the global
scope's data, but:

- `AddBreadcrumb(ctx, ...)` drops the breadcrumb. This differs from the previous 
behavior of applying to the `CurrentHub` that leaked breadcrumbs between events.
- `LastEventID(ctx)` returns an empty ID.
- `ScopeFromContext(ctx)` returns `nil`; check it before calling scope methods.

HTTP and gRPC server middleware create a scope for every request. In scripts,
workers, and other code without middleware, create one and pass its context on:

```go
// Before
sentry.AddBreadcrumb(&sentry.Breadcrumb{Message: "loaded config"})
sentry.CaptureException(err)

// After
ctx, _ := sentry.WithScope(context.Background())
sentry.AddBreadcrumb(ctx, &sentry.Breadcrumb{Message: "loaded config"})
sentry.CaptureException(ctx, err)
```

Events, logs, and metrics without a span use their scope's trace, which
`WithScope` copies. Transactions start their own trace unless the scope's trace
was continued. To give a unit of work its own trace, set
`scope.SetPropagationContext(sentry.NewPropagationContext())`.

### Capturing events

Add `ctx` as the first argument to `CaptureException`, `CaptureMessage`,
`CaptureEvent`, `CaptureCheckIn`, and `AddBreadcrumb`. Replace hub methods with
these package-level functions. `AddBreadcrumb` requires a context that carries
a scope; see [Contexts without a scope](#contexts-without-a-scope).

`WithMonitor` also takes a context, and `Hub.WithMonitor` is removed:

```go
// Before
err := sentry.WithMonitor("nightly-cleanup", monitorConfig, cleanup)

// After
err := sentry.WithMonitor(ctx, "nightly-cleanup", monitorConfig, cleanup)
```

Client capture methods also take a context. The separate `hint` and `scope`
arguments are replaced by a context and optional capture options:

```go
// Before
client.CaptureException(err, hint, scope)

// After
ctx = sentry.ContextWithScope(ctx, scope)
client.CaptureException(ctx, err, sentry.WithEventHint(hint))
```

Use `WithEventHint(hint)` in place of `hub.CaptureEventWithHint`, and
`WithBreadcrumbHint(hint)` for breadcrumb metadata. Use `WithLevel` to change
the level of a single capture:

```go
sentry.CaptureMessage(ctx, "Payment delayed", sentry.WithLevel(sentry.LevelWarning))
```

`EventModifier` and the client helpers `EventFromMessage`, `EventFromException`,
and `EventFromCheckIn` have been removed. Use the corresponding capture methods,
scope setters, and `BeforeSend` or event processors to customize events.

Explicit event tags and level now take precedence over scope values, and the
context's scope takes precedence over the global scope. `WithLevel` overrides
both event and scope levels.

For panic recovery, replace `Recover()` and `RecoverWithContext(ctx)` with a
directly deferred call:

```go
defer sentry.Recover(ctx, nil)
```

If you already called Go's `recover()`, pass its result as the second argument.

### Integrations and tracing

HTTP and gRPC server middleware fork a request scope, including when the incoming
context already carries a scope. In Gin, Echo, Fiber, Fiber v3, FastHTTP, and
Iris, `GetHubFromContext`, `SetHubOnContext`, and `GetSpanFromContext` are
replaced by `GetContext`, which returns the request's context:

```go
// Inside a handler after the Sentry Gin middleware.
ctx := sentrygin.GetContext(c)
sentry.ScopeFromContext(ctx).SetTag("page", "checkout")
sentry.CaptureException(ctx, err)
```

| Before | v1 |
| --- | --- |
| `scope.GetSpan()` or an integration's `GetSpanFromContext(c)` | `sentry.SpanFromContext(ctx)` for the active span, or `sentry.TransactionFromContext(ctx)` for the root transaction |
| `scope.SetSpan(span)` | Pass `span.Context()` to subsequent calls |
| `ContinueTrace(hub, trace, baggage)` | `ContinueFromHeaders(trace, baggage)` |
| `ContinueFromTrace(trace)` | `ContinueFromHeaders(trace, "")` |
| `ContinueFromRequest(r)` | Unchanged |
| `hub.GetTraceparent()` | `GetTraceparent(ctx)`, the `sentry-trace` header value |
| `hub.GetTraceparentW3C()` | `GetTraceparentW3C(ctx)`, the W3C `traceparent` header value |
| `hub.GetBaggage()` | `GetBaggage(ctx)` |

`GetTraceparent`, `GetTraceparentW3C`, and `GetBaggage` return an empty string
when the context carries neither a scope nor a span.

`Client.SetExternalContextTraceResolver` is removed. Add
`sentryotel.NewOtelIntegration()` to link OpenTelemetry traces.

Scopes no longer hold a span. Pass the span's context to link captures to it:

```go
span := sentry.StartSpan(ctx, "payment.authorize")
defer span.Finish()
sentry.CaptureException(span.Context(), err)
```

#### Outgoing HTTP and gRPC requests

An empty `TracePropagationTargets` slice now disables trace propagation instead
of allowing every URL. Leave it nil to propagate to every URL.

The gRPC client interceptor no longer starts a transaction for calls made
without a parent span; it only propagates the trace. Start a transaction and
pass its context if you want RPC spans recorded.

### Logs and metrics

Pass the request context to log and metric APIs as well. Replace
`WithScopeOverride(scope)` with
`meter.WithCtx(sentry.ContextWithScope(ctx, scope))`.

#### Logrus

`NewLogHook` now takes a context and levels, and no longer creates a client or
returns an error. Initialize the SDK with `sentry.Init`, or create a client with
`sentry.NewClient` and attach it to the context, before constructing the hook.
`NewLogHookFromClient` and `Hook.SetHubProvider` are removed. When wanting to link 
to an active trace use `logger.WithContext(ctx)` for each entry:

```go
// Before
hook := sentrylogrus.NewLogHookFromClient(levels, client)

// After
ctx = sentry.ContextWithClient(ctx, client)
hook := sentrylogrus.NewLogHook(ctx, levels)
logger.AddHook(hook)
logger.WithContext(ctx).Info("Job started")
```

`Hook.SetFallback` and `FallbackFunc` are removed. Logs are
sent asynchronously, so there is no failed capture to fall back from.

#### Zerolog

`Config` is removed. Replace both `New(Config{...})` and `NewWithHub(hub, opts)`
with `New(ctx, opts)`. Move the former `Config.ClientOptions` to `sentry.Init`
or `sentry.NewClient`; the writer uses the client selected by its context:

```go
// Before
writer, err := sentryzerolog.New(sentryzerolog.Config{
    ClientOptions: clientOptions,
    Options: writerOptions,
})

// After
client, err := sentry.NewClient(clientOptions)
if err != nil {
    return err
}
ctx = sentry.ContextWithClient(ctx, client)
writer, err := sentryzerolog.New(ctx, writerOptions)
```

### Transports

Custom transports now receive envelopes instead of events and the telemetry
buffer is always used. The `ClientOptions.DisableTelemetryBuffer` option is removed.

| Before | v1 |
| --- | --- |
| `Transport.SendEvent(*sentry.Event)` / built-in `SendEventWithContext` | `Transport.SendEnvelope(context.Context, *protocol.Envelope) error` |
| `Transport.Flush(timeout)` | `Transport.FlushWithContext(ctx)` |
| `HTTPTransport.BufferSize` counts events | `HTTPTransport.BufferSize` counts envelopes |

#### Custom transports

Import `github.com/getsentry/sentry-go/protocol` and replace `SendEvent` with
`SendEnvelope`. The full transport interface is:

```go
type Transport interface {
	Configure(options sentry.ClientOptions)
	SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error
	FlushWithContext(ctx context.Context) bool
	Close()
}
```

`Flush(timeout)` is removed from transports, including the built-in ones.
`sentry.Flush(timeout)` and `client.Flush(timeout)` are unchanged.

```go
// Before
transport.Flush(2 * time.Second)

// After
flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
transport.FlushWithContext(flushCtx)
```

## Migrating from raven-go

A [`raven-go` to `sentry-go` migration guide](https://docs.sentry.io/platforms/go/migration/) is available at the official Sentry documentation site.
