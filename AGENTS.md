# Go SDK

## Commit Attribution

AI commits MUST include:

```
Co-Authored-By: <agent model name> <agent-email-or-noreply@example.com>
```

## Code

- Everything exported outside `internal/`, in any module, is public API under semver. Don't break it unless asked. 
- Adding public API is easy, removing it isn't. Question every addition, even a requested one: export only what users need.
- Don't add dependencies to the root module or bump the Go version unless asked.

## Testing

- Default to using `internal/sentrytest` as a testing framework. Tests should follow a struct based simple approach and use the public API. Tests should also guarantee safe concurrency. We don't care to nil check and panics on tests; this equates to a test failure.
- No testify and asserts; we want to remove them. Prefer `cmp.Diff`, so the testify dependency can be dropped later.
- Prefer adding to an existing table-driver test before writing a new test function.
