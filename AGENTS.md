# Go SDK

## Public API

- Treat everything exported outside `internal/`, in any module, as public API under semantic versioning. Do not break it unless explicitly asked.
- Question every addition to the public API, including requested ones. Additions are easy to make and hard to remove, so export only what users need.
- Prefer the simplest API that leaves room to grow, so that later changes do not have to break it.
- Do not add dependencies to the root module or raise the Go version unless asked.

## SDK behavior

- Preserve host-application safety. The SDK must not panic, and must not block or perform expensive work on the caller's goroutine.
- Treat values passed to the SDK as shared with the caller. Callers may modify them after the call, so copy what the SDK retains. A shallow copy of maps or `any` values is not sufficient.
- Preserve compliance with the [SDK Development docs](https://develop.sentry.dev/sdk/). Where the specification is silent, align with the behavior of other Sentry SDKs before introducing new behavior.
- Be conservative on hot paths. Avoid unnecessary allocations, reflection, serialization, and blocking in the capture, log, and metric paths. Compare benchmarks with `benchstat` when changing them.
- Do not send data that may contain PII unless the user has opted in. When in doubt, omit it.
- Do not emit inaccurate telemetry. If a value cannot be measured correctly, skip it.
- Keep each integration in its own module, modeled on an existing integration, and register it in `go.work` and `.craft.yml`.
- Prefer minimal, concise changes over broad refactors or speculative cleanups.

## Testing

- Write tests with `internal/sentrytest` and exercise the public API.
- Prefer table-driven tests. Add a case to an existing table before writing a new test function.
- Compare values with `cmp.Diff`. Do not use testify in new tests. It is being phased out so that the dependency can be removed.
- For a bug fix, add a failing test that reproduces the bug before changing production code.
- Tests must pass with the race detector enabled, because the SDK is used concurrently.
- Do not add nil checks to tests. A panic already fails the test.

## Verification

- Use `make` to format, lint, vet, and test changes.

## Documentation and changelog

- Write comments only for intent, invariants, and non-obvious constraints. Do not add comments that restate the code.
- Keep doc comments to one or two lines that describe what the identifier does.
- The PR title becomes the changelog entry. To override it, add a `### Changelog Entry` section to the PR description.

## Security

- Do not open public issues or pull requests for known or suspected security vulnerabilities. Follow the private reporting process in `.github/SECURITY.md`.
