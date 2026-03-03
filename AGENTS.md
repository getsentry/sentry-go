# Sentry Go SDK

Single-module Go SDK with integration sub-modules in `github.com/getsentry/sentry-go`.

## Setup

- Requires **Go 1.24+** (tested on 1.24, 1.25, 1.26)
- After cloning: `make build`
- Never change Go version constraints in `go.mod` unless explicitly asked

## Commands

| Command                | Purpose                                    |
| ---------------------- | ------------------------------------------ |
| `make build`           | Build all modules                          |
| `make test`            | Run all tests (root + integrations)        |
| `make test-race`       | Run tests with `-race`                     |
| `make test-verbose`    | Run tests with `-v -race`                  |
| `make test-coverage`   | Run tests with coverage report             |
| `make vet`             | Run `go vet` on all modules                |
| `make lint`            | Run `golangci-lint`                        |
| `make fmt`             | Format all Go files with `gofmt -s`        |
| `make mod-tidy`        | Tidy all `go.mod` files and verify no diff |

Single integration: `cd <integration> && go test ./...`

## Commit Attribution

AI commits MUST include:

```
Co-Authored-By: <agent model name> <noreply@anthropic.com>
```

## Before Every Commit

1. `make fmt`
2. `make lint`
3. `make vet`
4. `make test-race`

## Architecture

### Core (`/`)

The root package `sentry` contains the entire public API. Key files:

- `sentry.go` — Package-level functions (`Init`, `CaptureException`, `CaptureMessage`, `Flush`)
- `client.go` — `Client` and `ClientOptions` (DSN, sampling, callbacks)
- `hub.go` — `Hub` manages a stack of `Scope`/`Client` pairs; thread-safe
- `scope.go` — `Scope` holds contextual data (tags, user, breadcrumbs, spans)
- `tracing.go` — `Span`, `StartSpan`, `StartTransaction`; W3C Trace Context propagation
- `transport.go` — `Transport` interface; async envelope delivery with buffering
- `interfaces.go` — `Event`, `Breadcrumb`, `User`, `Request`, `Exception` types

### Attribute Package (`/attribute/`)

Type-safe key-value builders used by structured logging and metrics:

```go
attribute.String("key", "value")
attribute.Int("count", 42)
attribute.Float64("ratio", 0.5)
attribute.Bool("flag", true)
```

### Internal Packages (`/internal/`)

Private implementation details — protocol encoding, rate limiting, telemetry buffer, debug logging. Do not export types from these packages.

### Integration Sub-Modules

Each integration lives in its own top-level directory with a separate `go.mod`. They fall into three categories:

- **HTTP middleware** — wrap handlers, create transaction spans, recover panics (e.g., `http/`, `gin/`, `echo/`, `fiber/`)
- **Logging hooks** — capture errors from log calls as Sentry events (e.g., `logrus/`, `zerolog/`, `zap/`, `slog/`)
- **Instrumentation** — outgoing HTTP spans (`httpclient/`), OpenTelemetry bridge (`otel/`)

Each sub-module follows the same pattern: a single exported middleware/hook constructor, options struct, and tests. When adding a new integration, mirror an existing one.

### Transport Architecture

The SDK has two transport layers. Understanding both is important when working on event delivery.

**Current: `transport.go` (active)**

The `HTTPTransport` is the production transport. It uses a batch-channel model with a single background worker goroutine:

- `SendEvent` serializes the event into an envelope and enqueues a `batchItem`
- A worker goroutine reads items sequentially and sends them via `http.Client`
- `Flush` closes the current batch's item channel, waits for the worker to drain it, then starts a new batch
- Rate limiting is tracked per-category via response headers
- `HTTPSyncTransport` is the blocking variant for serverless/FaaS use cases

**Next: `internal/telemetry/` + `internal/http/` (not yet enabled)**

A new processor/buffer/scheduler architecture designed to replace the above. Currently wired up in `client.go` (`setupTelemetryProcessor`) but **commented out** behind `DisableTelemetryBuffer`.

Key components:

- **`internal/telemetry/processor.go`** — top-level orchestrator; routes items to category-specific buffers
- **`internal/telemetry/scheduler.go`** — weighted round-robin scheduler; errors get 5x the processing slots of logs
- **`internal/telemetry/ring_buffer.go`** — circular buffer with overflow policies (drop-oldest/drop-newest), configurable batch size and flush timeout
- **`internal/telemetry/bucketed_buffer.go`** — groups items by trace ID for trace-aware batching
- **`internal/http/transport.go`** — `AsyncTransport` implementing `protocol.TelemetryTransport` (envelope-first interface with `HasCapacity()` backpressure)
- **`internal/protocol/`** — `Envelope`, `TelemetryItem`, `EnvelopeItemConvertible` interfaces; batch types for logs and metrics

The `internalAsyncTransportAdapter` in `transport.go` bridges the old `Transport` interface to the new `TelemetryTransport` interface.

### Examples (`/_examples/`)

Runnable example programs for each feature and integration. Reference these for idiomatic usage patterns.

## Coding Standards

- Follow existing conventions — check neighboring files before adding new patterns
- Only use libraries already present in `go.mod`; do not add new dependencies without asking
- Run `gofmt -s` — the project uses standard Go formatting, no custom style
- Exported identifiers need doc comments per [Go convention](https://go.dev/blog/godoc)
- Keep the public API surface in the root package; internals go in `/internal`
- Use `context.Context` for cancellation and span propagation
- Ensure thread safety — the SDK is used concurrently; guard shared state with mutexes
- When modifying behavior, update tests in the corresponding `*_test.go` files
- Never expose secrets or DSN tokens in test fixtures

## Testing

See **[.agents/TESTING.md](.agents/TESTING.md)** for full guidelines. Key points:

- Prefer integration tests that use real APIs (`sentry.Init`, `CaptureException`, `Flush`) over mocking internals
- Tests use the standard `testing` package with [`testify`](https://github.com/stretchr/testify) assertions
- Internal test helpers live in `internal/testutils/` (mocks, assertions, constants)
- Always run `make test-race` before submitting — the SDK must be free of data races
- Use `t.Parallel()` where tests are independent
- Prefer table-driven tests for covering multiple cases

## Reference Documentation

- [SDK Development Guide](https://develop.sentry.dev/sdk/)
- [Commit Guidelines](https://develop.sentry.dev/engineering-practices/commit-messages/)
- [Span Attributes](https://develop.sentry.dev/sdk/foundations/state-management/scopes/attributes/)
- [Hubs & Scopes](https://develop.sentry.dev/sdk/foundations/state-management/hub-and-scope/)

## Skills

### Commit

Use `/commit` skill when committing changes. Follows Sentry conventional commit format.

### Code Review

Use `/code-review` skill to review pull requests following Sentry engineering practices.

### Find Bugs

Use `/find-bugs` skill to audit local branch changes for bugs and security issues.
