# Migration Guide

## Upgrading to v1

The v1 version of the SDK introduces a new, ergonomic API built around Go's
`context.Context`. Isolation is now the default: every request gets its own
scope, so there's no `Hub` to clone and no tags or breadcrumbs leaking between
goroutines. Linking just works, too. Pass the context you already have, and
errors, logs, and metrics land on the right trace without any bookkeeping.
Every framework integration hands you the request's context, so your capture
code reads the same on net/http, gRPC, Gin, or Fiber. For most code, upgrading
means passing `ctx` where you already have it.

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

The sections below cover what to update. [All changes](#all-changes) lists every
removed and replaced API.

### Scopes

`WithScope(ctx)` forks the scope carried by `ctx`, or starts one, and returns a
context that carries it. Middleware does this for every request. In scripts and
workers, create a scope and keep passing its context:

```go
ctx, _ := sentry.WithScope(context.Background())
sentry.AddBreadcrumb(ctx, &sentry.Breadcrumb{Message: "loaded config"})
sentry.CaptureException(ctx, err)
```

A context without a scope, such as `context.Background()`, still captures
events, but drops breadcrumbs and has no last event ID.

### Capturing events

Pass the context first to `CaptureException`, `CaptureMessage`, `CaptureEvent`,
`CaptureCheckIn`, `AddBreadcrumb`, and `WithMonitor`. Levels and hints are
capture options:

```go
sentry.CaptureMessage(ctx, "Payment delayed", sentry.WithLevel(sentry.LevelWarning))
client.CaptureException(ctx, err, sentry.WithEventHint(hint))
```

Recover from panics with `defer sentry.Recover(ctx, nil)`. If you already called
`recover()`, pass its result instead of `nil`.

### Integrations

Integrations fork a scope for every request. Get the request's context from the
integration, and use it like any other context:

```go
ctx := sentrygin.GetContext(c) // r.Context() for net/http, the handler's ctx for gRPC
sentry.ScopeFromContext(ctx).SetTag("page", "checkout")
sentry.CaptureException(ctx, err)
```

### Tracing

Spans travel in the context. Pass `span.Context()` to work that belongs to the
span:

```go
span := sentry.StartSpan(ctx, "payment.authorize")
defer span.Finish()
sentry.CaptureException(span.Context(), err)
```

Continue incoming traces with `ContinueFromRequest(r)` or
`ContinueFromHeaders(trace, baggage)`.

### Logs and metrics

Pass the context to loggers and meters. The Logrus and Zerolog integrations take
a context and use its client instead of creating one:

```go
ctx = sentry.ContextWithClient(ctx, client)

// Logrus
logger.AddHook(sentrylogrus.NewLogHook(ctx, levels))
logger.WithContext(ctx).Info("Job started")

// Zerolog
writer, err := sentryzerolog.New(ctx, writerOptions)
```

### Transports

Custom transports send envelopes and flush with a context. Import
`github.com/getsentry/sentry-go/protocol` and implement:

```go
type Transport interface {
	Configure(options sentry.ClientOptions)
	SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error
	FlushWithContext(ctx context.Context) bool
	Close()
}
```

`sentry.Flush(timeout)` and `client.Flush(timeout)` are unchanged.

### All changes

<details>
<summary>Scopes and hubs</summary>

| Before | v1 |
| --- | --- |
| `CurrentHub().Scope()` or global `ConfigureScope(...)` | `GlobalScope()` for application-wide data |
| `GetHubFromContext(ctx).Scope()` | `ScopeFromContext(ctx)` |
| `HasHubOnContext(ctx)` | `ScopeFromContext(ctx) != nil` |
| `hub.Clone()` or callback-based `WithScope(...)` | `ctx, scope := sentry.WithScope(ctx)` |
| `PushScope()` and `PopScope()` | `WithScope(ctx)` for the temporary work, then keep using `ctx` |
| `NewHub(client, scope)` and `SetHubOnContext(ctx, hub)` | `ContextWithScope(ctx, scope)` and `ContextWithClient(ctx, client)` |
| `hub.Client()` | `ClientFromContext(ctx)` |
| `hub.BindClient(client)` | `ctx = sentry.ContextWithClient(ctx, client)` |
| `LastEventID()` or `hub.LastEventID()` | `LastEventID(ctx)` |
| `hub.Flush(timeout)` or `hub.FlushWithContext(ctx)` | `ClientFromContext(ctx).Flush(timeout)` or `FlushWithContext(ctx)` |

</details>

<details>
<summary>Capturing</summary>

| Before | v1 |
| --- | --- |
| `CaptureException`, `CaptureMessage`, `CaptureEvent`, `CaptureCheckIn`, `AddBreadcrumb` | The same functions with `ctx` as the first argument |
| Hub capture methods, such as `hub.CaptureException(err)` | The package-level functions with `ctx` |
| `hub.CaptureEventWithHint(event, hint)` | `CaptureEvent(ctx, event, sentry.WithEventHint(hint))` |
| `client.CaptureException(err, hint, scope)` and the other client capture methods | `client.CaptureException(ctx, err, sentry.WithEventHint(hint))`, with `ContextWithScope(ctx, scope)` |
| Breadcrumb hint argument | `AddBreadcrumb(ctx, breadcrumb, sentry.WithBreadcrumbHint(hint))` |
| `WithMonitor(slug, config, fn)` and `hub.WithMonitor` | `WithMonitor(ctx, slug, config, fn)` |
| `Recover()` and `RecoverWithContext(ctx)` | `defer sentry.Recover(ctx, nil)` |
| `EventModifier`, `client.EventFromMessage`, `EventFromException`, `EventFromCheckIn` | Capture methods, scope setters, and `BeforeSend` or event processors |

</details>

<details>
<summary>Integrations</summary>

`GetHubFromContext`, `SetHubOnContext`, and `GetSpanFromContext` are replaced by
`GetContext`, which returns the request's context.

| Integration | Request context |
| --- | --- |
| net/http, Negroni | `r.Context()` |
| gRPC | The handler's `ctx` argument |
| Gin | `sentrygin.GetContext(c)` |
| Echo | `sentryecho.GetContext(c)` |
| Fiber and Fiber v3 | `sentryfiber.GetContext(c)` from the matching integration module |
| FastHTTP | `sentryfasthttp.GetContext(c)` |
| Iris | `sentryiris.GetContext(c)` |

</details>

<details>
<summary>Tracing</summary>

| Before | v1 |
| --- | --- |
| `scope.GetSpan()` or an integration's `GetSpanFromContext(c)` | `SpanFromContext(ctx)`, or `TransactionFromContext(ctx)` for the root |
| `scope.SetSpan(span)` | Pass `span.Context()` to subsequent calls |
| `ContinueTrace(hub, trace, baggage)` | `ContinueFromHeaders(trace, baggage)` |
| `ContinueFromTrace(trace)` | `ContinueFromHeaders(trace, "")` |
| `hub.GetTraceparent()` | `GetTraceparent(ctx)` |
| `hub.GetTraceparentW3C()` | `GetTraceparentW3C(ctx)` |
| `hub.GetBaggage()` | `GetBaggage(ctx)` |

</details>

<details>
<summary>Logs and metrics</summary>

| Before | v1 |
| --- | --- |
| `sentry.WithScopeOverride(scope)` meter option | `meter.WithCtx(sentry.ContextWithScope(ctx, scope))` |
| `sentrylogrus.NewLogHook(levels, clientOptions)` | `sentry.Init` or `sentry.NewClient`, then `NewLogHook(ctx, levels)` |
| `sentrylogrus.NewLogHookFromClient(levels, client)` | `NewLogHook(sentry.ContextWithClient(ctx, client), levels)` |
| `Hook.SetHubProvider` | `logger.WithContext(ctx)` for each entry |
| `Hook.SetFallback` and `FallbackFunc` | Removed; logs are sent asynchronously |
| `sentryzerolog.New(Config{...})` and `NewWithHub(hub, opts)` | `sentryzerolog.New(ctx, opts)` |
| `sentryzerolog.Config.ClientOptions` | `sentry.Init` or `sentry.NewClient` |

</details>

<details>
<summary>Transports</summary>

| Before | v1 |
| --- | --- |
| `Transport.SendEvent(*sentry.Event)` / built-in `SendEventWithContext` | `Transport.SendEnvelope(context.Context, *protocol.Envelope) error` |
| `Transport.Flush(timeout)` | `Transport.FlushWithContext(ctx)` |
| `HTTPTransport.BufferSize` counts events | `HTTPTransport.BufferSize` counts envelopes |
| `ClientOptions.DisableTelemetryBuffer` | Removed; the telemetry buffer is always used |

</details>

## Migrating from raven-go

A [`raven-go` to `sentry-go` migration guide](https://docs.sentry.io/platforms/go/migration/) is available at the official Sentry documentation site.
