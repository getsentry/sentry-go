---
name: sdk-review
description: "Review sentry-go changes for violations of Sentry SDK contracts: host-app safety, data-collection gates, dependency versions, public API, context propagation, flush deadlines, client reports and event types."
allowed-tools: Read Grep Glob
---
Report a finding only if you can show how the change breaks a user's app, build or data. Name the fix: the gate, guard or deprecation path to add.

## Rules

1. **Never break the host app** (high). SDK code that can panic, block, or change the user's behavior: returned errors or values, request or job flow.
2. **Data collected without its gate** (high). Bodies, headers, cookies, user info, `gen_ai` inputs/outputs or DB results recorded without their data-collection option. Header and cookie names match case-insensitively. Gate on `ClientOptions.DataCollection`; `SendDefaultPII` is deprecated. Automatically collected header, cookie, query or body values must go through the denylist filter in `data_collection_filter.go`; a new path that sends them raw is a finding. Data the user set explicitly (scope, span, `BeforeSend`) is never gated or scrubbed: don't flag reading it back from the scope, but do flag new code that gates or rewrites it.
3. **Dependency versions** (high). Third-party dependency APIs newer than the version the module's own `go.mod` requires. CI builds every module through `go.work`, so a newer version that another module requires can hide the gap.
4. **Public API** (medium). Breaking public changes without a deprecation path, or internals exposed as public API.
5. **Context propagation** (high). An integration must fork the scope from the incoming request and pass `context.Context` on, calls should reuse `context.Context`, logs and metrics should be linked using `WithCtx` and `sentry-trace` / `baggage` should agree with the span's sampling decision.
6. **Flush deadline** (high). `Flush` / `FlushWithContext` must bound draining and sending, not only the final transport flush, and return false once the deadline passes. `Close` dropping queued items and rejecting late ones is intended; leaked goroutines are not.
7. **Client reports** (medium). Dropped items (rate limit, sampling, `BeforeSend`) that aren't recorded, or are recorded under the wrong category.
8. **Event types** (medium). Check-ins, logs or metrics going through error sampling or `BeforeSend`, or losing their type.

## Do not report

- Anything CI already fails on: compile and type errors, language features or standard library APIs newer than Go 1.26 (`test.yml` builds on Go 1.26 and 1.27), `go vet`, untidy `go.mod` files (`make mod-tidy`), known-vulnerable dependencies (`govulncheck`), and the `golangci-lint` checks in `.golangci.yml`, such as unchecked errors (`errcheck`), unclosed response bodies (`bodyclose`), `gosec`, `staticcheck`, unused code, and formatting (`gofmt`, `goimports`).
- A trade-off or follow-up (stacked PR, major branch) that the PR description explains.
- Edge cases that need misuse or very unlikely timing.
