# Sentry Go SDK

Single-module Go SDK with integration sub-modules in `github.com/getsentry/sentry-go`.

## Setup

- Requires **Go 1.24+** (tested on 1.24, 1.25, 1.26)
- After cloning: `make build`
- Never change Go version constraints in `go.mod` unless explicitly asked

## Commit Attribution

AI commits MUST include:

```
Co-Authored-By: <agent model name> <noreply@anthropic.com>
```

## Before Every Commit

1. `make fmt` 2. `make lint` 3. `make vet` 4. `make test-race`

## Architecture

### Core (`/`)

The root package `sentry` contains the entire public API:

- `sentry.go` — `Init`, `CaptureException`, `CaptureMessage`, `Flush`
- `client.go` — `Client`, `ClientOptions`
- `hub.go` — `Hub` manages a stack of `Scope`/`Client` pairs; thread-safe
- `scope.go` — `Scope` holds contextual data (tags, user, breadcrumbs, spans)
- `tracing.go` — `Span`, `StartSpan`, `StartTransaction`; W3C Trace Context
- `transport.go` — `Transport` interface and `HTTPTransport`/`HTTPSyncTransport`
- `interfaces.go` — `Event`, `Breadcrumb`, `User`, `Request`, `Exception`

### Attribute Package (`/attribute/`)

Type-safe key-value builders used by structured logging and metrics:

```go
attribute.String("key", "value")
attribute.Int("count", 42)
attribute.Float64("ratio", 0.5)
attribute.Bool("flag", true)
```

### Internal Packages (`/internal/`)

Private implementation. Do not export types from these packages.

### Integration Sub-Modules

Each lives in its own directory with a separate `go.mod`:

- **HTTP middleware** — `http/`, `gin/`, `echo/`, `fiber/`, `fasthttp/`, `iris/`, `negroni/`
- **Logging hooks** — `logrus/`, `zerolog/`, `zap/`, `slog/`
- **Instrumentation** — `httpclient/`, `otel/`

When adding a new integration, mirror an existing one.

### Transport Architecture

**Current: `transport.go` (active)** — `HTTPTransport` uses a batch-channel model with a single worker goroutine. `HTTPSyncTransport` is the blocking variant for serverless.

**Next: `internal/telemetry/` + `internal/http/` (not yet enabled)** — Processor/buffer/scheduler architecture. Wired up in `client.go` (`setupTelemetryProcessor`) but **commented out** behind `DisableTelemetryBuffer`. Key parts:

- `internal/telemetry/processor.go` — orchestrator; routes items to category-specific buffers
- `internal/telemetry/scheduler.go` — weighted round-robin; errors get 5x priority over logs
- `internal/telemetry/ring_buffer.go` — circular buffer with overflow policies and batch/timeout flushing
- `internal/telemetry/bucketed_buffer.go` — groups items by trace ID
- `internal/http/transport.go` — `AsyncTransport` with `HasCapacity()` backpressure
- `internal/protocol/` — `Envelope`, `TelemetryItem` interfaces; log/metric batch types

The `internalAsyncTransportAdapter` in `transport.go` bridges old `Transport` to new `TelemetryTransport`.

## Coding Standards

- Follow existing conventions — check neighboring files first
- Do not add new dependencies without asking
- `gofmt -s` formatting, doc comments on exports
- Public API in root package; internals in `/internal`
- Thread safety required — guard shared state with mutexes
- Update tests when modifying behavior

## Testing

**Prefer tests that exercise real user-facing behavior over isolated unit tests.** Call the same APIs users call — `sentry.Init`, `CaptureException`, `Flush`, framework middleware — rather than mocking internal components.

Test tier preference (use the highest tier that covers what you need):

1. **Integration tests** — `sentry.Init` with `BeforeSend` callbacks, `httptest.Server` with real framework routers, `sentry.Flush` to collect events. This is the default for any new test.
2. **Context-level tests** — `NewTestContext` with `MockTransport` for span/transaction behavior when no HTTP server is needed.
3. **Unit tests** (sparingly) — Direct `NewClient` + `MockScope` only for self-contained logic like `BeforeSend` callbacks or sampling decisions.

Conventions:

- Table-driven tests for multiple inputs through the same code path
- `t.Parallel()` for tests that don't share global state
- `cmp.Diff` with `cmpopts.IgnoreFields` for `*Event` comparison — ignore `EventID`, `Timestamp`, `Sdk`, `sdkMetaData`
- `testutils.FlushTimeout()` when calling `sentry.Flush` (longer timeout in CI)
- `testify` for assertions, `internal/testutils/` for mocks
- All tests must pass `make test-race`

What to test:

- Behavior users observe: Does middleware capture panics? Does `Flush` deliver events? Do trace headers propagate?
- Edge cases at system boundaries: malformed DSN, nil `Hub`, concurrent captures, context cancellation
- Regressions: reproduce the failure before applying the fix

Thread safety:

- The SDK is used concurrently. Any test touching shared state (`Hub`, `Scope`, `CurrentHub`) must either use `t.Parallel()` with isolated instances, or explicitly verify safety with goroutines and `sync.WaitGroup`.

## Reference

- [SDK Development Guide](https://develop.sentry.dev/sdk/)
- [Commit Guidelines](https://develop.sentry.dev/engineering-practices/commit-messages/)
- [Hubs & Scopes](https://develop.sentry.dev/sdk/unified-api/#hub)

## Skills

- `/commit` — Commit with Sentry conventional format
- `/create-pr` — Create PRs following Sentry conventions
- `/code-review` — Review PRs following Sentry practices
- `/find-bugs` — Audit local changes for bugs and security issues