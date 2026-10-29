# Migration Guide

## Upgrading to v1

The v1 version of the SDK introduces a new, ergonomic API built around Go's
`context.Context`. Request isolation now works by default. You pass `context.Context` and
the SDK uses it to decide which scope and which trace an event belongs to, without the need
to clone a `Hub`. Linking works through the same context, so that errors, logs and metrics
are attached to the request's trace and show up linked in Sentry.

v1 also includes transport improvements. Telemetry buffers are now always enabled, which
moves serialization to the background and sends events by priority to limit drops.

> [!NOTE]
> v1 is in alpha. Let us know how the upgrade goes in the
> [v1 alpha feedback issue](https://github.com/getsentry/sentry-go/issues/1463).

The following sections cover the changes most applications need to make.
[Appendix: All API Breakages](#appendix-all-api-breakages) maps every removed or
changed API to its v1 replacement.

### Scopes

`WithScope(ctx)` forks the scope carried by `ctx`, or starts one, and returns a
context that carries it. Middlewares do this for every request. For isolated work,
fork a scope from the context you already have and keep passing the returned context:

```go
func processJob(ctx context.Context, job Job) {
	ctx, scope := sentry.WithScope(ctx)
	scope.SetTag("job", job.Name)
	sentry.AddBreadcrumb(ctx, &sentry.Breadcrumb{Message: "job started"})
	if err := job.Run(ctx); err != nil {
		sentry.CaptureException(ctx, err)
	}
}
```

Start from `context.Background()` only when there is no context to pass down, such
as in `main`. A context without a scope still captures events, but drops
breadcrumbs and has no last event ID.

If you are new to `context.Context`, see the [`context` package documentation](https://pkg.go.dev/context)
and [Go Concurrency Patterns: Context](https://go.dev/blog/context).

### Capturing events

`CaptureException`, `CaptureMessage`, `CaptureEvent`, `CaptureCheckIn`, `AddBreadcrumb`,
and `WithMonitor` now take `context.Context` as their first argument. Hints, which
used to be required arguments or separate methods such as `CaptureEventWithHint`,
are now optional trailing options. Use `WithEventHint` and `WithLevel` for a single
capture, and `WithBreadcrumbHint` for a single breadcrumb:

```go
sentry.CaptureMessage(ctx, "Payment delayed", sentry.WithLevel(sentry.LevelWarning))
client.CaptureException(ctx, err, sentry.WithEventHint(hint))
```

### Integrations

Integrations fork a scope for every request. Get the request's context from the
integration, and use it like any other context:

```go
// net/http
func handler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sentry.ScopeFromContext(ctx).SetTag("page", "checkout")
	sentry.CaptureException(ctx, err)
}

// Gin
func handler(c *gin.Context) {
	ctx := sentrygin.GetContext(c)
	sentry.CaptureException(ctx, err)
}

// gRPC
func (s *server) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
	sentry.CaptureException(ctx, err)
}
```

The appendix lists how to get the request's context for every integration.

### Tracing

Starting a span no longer changes the scope. Previously, `StartSpan` stored the span
on the hub's scope, and any later capture through that hub was linked to it, even
from another goroutine. Now an event is linked only to the span carried by the
context you pass, so pass `span.Context()` instead of the context you started the
span from:

```go
span := sentry.StartSpan(ctx, "payment.authorize")
defer span.Finish()
sentry.CaptureException(span.Context(), err)
```

The integrations continue incoming traces for you. Without an integration, start the
transaction with `ContinueFromRequest(r)`, or with `ContinueFromHeaders(trace, baggage)`.

### Logs and metrics

Pass the context to loggers and meters. The Logrus and Zerolog integrations take
a context and use its client instead of creating one, to align with the other integrations:

```go
ctx = sentry.ContextWithClient(ctx, client)

// Logrus
logger.AddHook(sentrylogrus.NewLogHook(ctx, levels))
logger.WithContext(ctx).Info("Job started")

// Zerolog
writer, err := sentryzerolog.New(ctx, writerOptions)
```

### Transports

Custom transports send envelopes and flush with a context. The new async
transport includes priority scheduling, background serialization and flushing
fixes. To create a custom transport, import `github.com/getsentry/sentry-go/protocol` and implement:

```go
type Transport interface {
	Configure(options sentry.ClientOptions)
	SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error
	FlushWithContext(ctx context.Context) bool
	Close()
}
```

### Appendix: All API Breakages

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
| `sentry.MockLogEntry` and `sentry.NewMockLogEntry` | Removed; define a fake that implements `sentry.LogEntry` in your tests |

</details>

<details>
<summary>Transports</summary>

| Before | v1 |
| --- | --- |
| `Transport.SendEvent(*sentry.Event)` / built-in `SendEventWithContext` | `Transport.SendEnvelope(context.Context, *protocol.Envelope) error` |
| `Transport.Flush(timeout)` | Removed; use `FlushWithContext(ctx)` |
| `HTTPTransport.BufferSize` counts events | `HTTPTransport.BufferSize` counts envelopes |
| `ClientOptions.DisableTelemetryBuffer` | Removed; telemetry is always buffered, including with custom transports |
| `Dsn.RequestHeaders()` and `protocol.Dsn.RequestHeaders(sdkVersion)` | Removed; they built `/store` endpoint headers. The SDK transport authenticates against the `/envelope` endpoint automatically |

</details>

### Feedback

We would love to hear your experience while trying out the SDK. You can leave your feedback under [v1 alpha](https://github.com/getsentry/sentry-go/issues/1463).

## Migrating from raven-go

A [`raven-go` to `sentry-go` migration guide](https://docs.sentry.io/platforms/go/migration/) is available at the official Sentry documentation site.
