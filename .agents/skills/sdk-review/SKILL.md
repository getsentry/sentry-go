---
name: sdk-review
description: "Review sentry-go changes for violations of Sentry SDK contracts: host-app safety, data-collection gates, supported versions, public API, and Go-specific rules."
allowed-tools: Read Grep Glob
---
Review only the changed code. Report a finding only if you can point to the exact line and show how it breaks a user's app, build or data. Most hunks break no SDK contract, so no findings is the normal result. Generic logic bugs belong to `code-review`; this skill only covers SDK contracts.

## Core SDK rules (same in every SDK repo)

1. **Never break the host app** (high). SDK code that can raise, block, or change the user's behavior: exceptions, return values, signatures, job or request flow.
2. **Data collected without its gate** (high). Bodies, headers, cookies, user info, `gen_ai` inputs/outputs or DB results recorded without their data-collection option. Header and cookie names match case-insensitively. Gate on `ClientOptions.DataCollection`; `SendDefaultPII` is deprecated.
3. **Supported versions** (high). Language, runtime, target or dependency APIs newer than the repo's declared minimum, used without a guard. Minimums come from the `go` line in the root `go.mod` and in each integration's own `go.mod`.
4. **Public API** (medium). Breaking public changes without a deprecation path, or internals exposed as public API.

## Do not report (same in every SDK repo)

- Style, naming, refactors, or anything the repo's linters, type checkers or analyzers catch.
- Pre-existing code the diff does not change.
- Behavior that matches the spec, Relay or other SDKs, or a trade-off the PR or its threads explain.
- Follow-ups the PR says are handled in a stacked PR or the major branch.
- Tests, samples and examples, unless they can't fail or they teach wrong SDK usage.
- Edge cases that need misuse or very unlikely timing.

## Go rules

5. **Context propagation** (high). An integration that drops the request's hub, scope or span (`context.Background()` or the global hub where a request context exists), or `sentry-trace` / `baggage` that disagree with the span's sampling decision.
6. **Flush deadline** (high). `Flush` / `FlushWithContext` must bound draining and sending, not only the final transport flush, and return false once the deadline passes. `Close` dropping queued items and rejecting late ones is intended; leaked goroutines are not.
7. **Client reports** (medium). Dropped items (rate limit, sampling, `BeforeSend`) that aren't recorded, or are recorded under the wrong category. Queue-overflow outcomes are recorded in the scheduler, not the transport, and items rejected after `Close` need none.
8. **Event types** (medium). Feedback, check-ins, logs or metrics going through error sampling or `BeforeSend`, or losing their type.
