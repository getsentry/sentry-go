# Sentry Go SDK

Single-module Go SDK with integration sub-modules in `github.com/getsentry/sentry-go`.

## Setup

- Requires **Go 1.24+** (tested on 1.24, 1.25, 1.26)
- After cloning: `make build`
- Never change Go version constraints in `go.mod` unless explicitly asked

## Commands

| Command          | Purpose                       |
| ---------------- | ----------------------------- |
| `make build`     | Build all modules             |
| `make test`      | Run all tests                 |
| `make test-race` | Run tests with `-race`        |
| `make vet`       | Run `go vet`                  |
| `make lint`      | Run `golangci-lint`           |
| `make fmt`       | Format with `gofmt -s`        |
| `make mod-tidy`  | Tidy and verify all `go.mod`s |

Single integration: `cd <integration> && go test ./...`

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

See **[.agents/TESTING.md](.agents/TESTING.md)** for full guidelines.

Prefer integration tests using real APIs (`sentry.Init`, `CaptureException`, `Flush`) over mocking internals. Use `testify` for assertions, `internal/testutils/` for mocks, and always pass `make test-race`.

## Reference

- [SDK Development Guide](https://develop.sentry.dev/sdk/)
- [Commit Guidelines](https://develop.sentry.dev/engineering-practices/commit-messages/)
- [Hubs & Scopes](https://develop.sentry.dev/sdk/unified-api/#hub)

## Skills

- `/commit` — Commit with Sentry conventional format
- `/create-pr` — Create PRs following Sentry conventions
- `/code-review` — Review PRs following Sentry practices
- `/find-bugs` — Audit local changes for bugs and security issues