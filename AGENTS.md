# Sentry Go SDK

Single-module Go SDK with integration sub-modules in `github.com/getsentry/sentry-go`.

## Commit Attribution

AI commits MUST include:

```
Co-Authored-By: <agent model name> <agent-email-or-noreply@example.com>
```

## Before Every Commit

1. `make fmt` 2. `make lint` 3. `make vet` 4. `make test-race`

## Architecture

### Core (`/`)

The root package `sentry` contains the client and transport APIs. The public
`protocol/` package owns wire envelopes, payload categories, DSNs, and SDK metadata.

### Attribute Package (`/attribute/`)

Type-safe key-value builders used by structured logging and metrics:

```go
attribute.String("key", "value")
attribute.Int("count", 42)
attribute.Float64("ratio", 0.5)
attribute.Bool("flag", true)
```

### Integration Sub-Modules

Each lives in its own directory with a separate `go.mod`:

- **HTTP middleware** — `http/`, `gin/`, `echo/`, `fiber/`, `fasthttp/`, `iris/`, `negroni/`
- **Logging hooks** — `logrus/`, `zerolog/`, `zap/`, `slog/`
- **Instrumentation** — `httpclient/`, `otel/`

When adding a new integration, mirror an existing one.

### Transport Architecture

The only delivery path is the telemetry processor followed by an envelope
transport. Custom transports use the same processor as the default transport.

- `transport_interface.go` defines the public transport contract using
  `protocol` types. Existing root DSN and SDK metadata aliases remain for compatibility.
- `transport.go` owns HTTP constructors, their options, and the private implementation.
  Recorder/provider/SDK dependencies are passed privately.
- `internal/telemetry/` owns buffering, scheduling, client report emission,
  item containers, and flushing. Its interfaces describe only the
  processing and delivery behavior it consumes.
- `internal/ratelimit/` owns rate-limit parsing, backoff, and scheduling priorities.
  Publicly constructed HTTP transports also emit their own delivery-loss reports.
- `mock_transport.go` captures envelopes; `internal/sentrytest` fixtures exercise
  the full processor and expose envelopes plus a decoded event view.

Clients are selected through context independently of scopes. Isolation scopes
snapshot their parent or global scope; transport workers never resolve scopes.

## Coding Standards

- Follow existing conventions — check neighboring files first
- Maintain existing Go versions and dependencies unless explicitly asked to change them
- `gofmt -s` formatting, doc comments on exports
- Public API in root package; internals in `/internal`
- Thread safety required — guard shared state with mutexes
- Update tests when modifying behavior

## Testing

Test tier preference (use the highest tier that covers what you need):

1. **Integration tests** (default) — Prefer `internal/sentrytest` with `sentrytest.Run` or `sentrytest.NewFixture`, plus real routers / `httptest` requests where needed. Prefer tests that use the public API.
2. **Context-level tests** — Prefer `sentrytest.NewContext` or `fixture.NewContext(parent)` for tracing / context propagation tests. Prefer `sentrytest.NewFixture` for isolated client + hub setup when no HTTP server is needed.
3. **Unit tests** (sparingly) — Direct `NewClient` + `MockScope` only for self-contained logic where `sentrytest` would add unnecessary indirection.

Conventions:

- Table-driven tests for multiple inputs through the same code path
- `t.Parallel()` for tests that don't share global state
- `cmp.Diff` with `cmpopts.IgnoreFields` for `*Event` comparison — ignore `EventID`, `Timestamp`, `Sdk`, `sdkMetaData`
- Prefer `fixture.Flush()` over direct `sentry.Flush(...)` in tests built on `internal/sentrytest`
- Prefer `fixture.Events()` as the captured event stream; inspect `event.Type` in assertions instead of relying on separate fixture streams
- `testify` for assertions, `internal/testutils/` for non-assert test helpers like mocks and flush timing
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
