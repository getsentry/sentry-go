# PR #1427: base/head reproductions

This directory contains diagnostic tests for [PR #1427](https://github.com/getsentry/sentry-go/pull/1427). It is intended for a draft PR targeting `feat/envelope-transport`, not for inclusion in the SDK test suite. Suggested inline review comments are in [comments.md](comments.md).

## Run

From the repository root:

```bash
bash review-repros/pr-1427/run.sh
```

Requires Bash, Git, Go, `tar`, and a writable `$TMPDIR`. The runner defaults to Go 1.25.0; Go may download that toolchain and module dependencies. Set `GOTOOLCHAIN` to another Go 1.25+ version if needed. Race detection requires a supported host and C toolchain.

The runner exports two committed SDK versions into temporary directories, copies this standalone Go module, and runs both diagnostics separately against each version. It does not change branches, modify the SDK, or make HTTP requests. It ignores uncommitted SDK changes.

Defaults are pinned so a new commit containing these reproduction files does not change the comparison:

- The immediate stack base is [0fdec1f](https://github.com/getsentry/sentry-go/commit/0fdec1ff5455ea9f208eccb57f74d4c9ff747ce5).
- The reviewed PR head is [4fd76f1](https://github.com/getsentry/sentry-go/commit/4fd76f13b03771b0e3daf4e0b0a965a971b99b57).

To compare a later committed fix against the same base:

```bash
bash review-repros/pr-1427/run.sh 0fdec1f <fix-commit>
```

The [base adapter](adapter_base_test.go) requires the legacy `NewHTTPSyncTransport` API and uses the `legacy` build tag; the [head adapter](adapter_head_test.go) requires `NewSyncTransport` and is selected by default. The [shared tests](repro_test.go) are otherwise identical.

This directory has its own [go.mod](go.mod) and is not included in the root workspace, so normal SDK builds and tests do not include these reproductions. The local replacement points to the SDK checkout for Go tooling. To run directly against that checkout:

```bash
(cd review-repros/pr-1427 && GOWORK=off go test -race -count=1 -v .)
```

These are diagnostic tests: failures are expected on the reviewed head. Use the comparison runner to test both committed versions. No SDK dependency or workspace configuration changes are needed.

## Verified results

The complete runner was verified on macOS arm64 with Go 1.25.0:

| Diagnostic | Stack base | PR head |
| --- | --- | --- |
| `TestFlushDeadline` | PASS: `elapsed=1s result=true` | FAIL: `elapsed=30s result=true` |
| `TestImmediateClose` | PASS: 1,000 immediate closes | FAIL: synctest deadlock, scheduler blocked in `sync.Cond.Wait` |

The runner returns **exit status 1 when any diagnostic fails**, including the expected reproduced bugs. Inspect the individual logs to distinguish a reproduced bug from a compilation or environment failure. The runner prints the temporary output directory; it retains exported sources, individual logs, and `summary.txt` there for inspection. Delete that directory manually when finished.

### Flush deadline

One log is buffered and then flushed with a one-second deadline. A fake HTTP transport stalls for 30 seconds or until its request context is canceled. `testing/synctest` uses simulated time, so neither version needs to wait 30 real seconds for the request. The base can return while its background sender continues; deferred close allows that sender to finish.

Only deadline compliance is asserted. Both versions return `true` despite deadline expiration, so asserting `false` would obscure the timing regression by failing on both versions. This is a comparison of the old and new client-facing synchronous transport paths, not a comparison of identically named implementations.

### Immediate shutdown

The test creates a client with `MockTransport` and immediately closes it inside a synctest environment, repeated 1,000 times. Synctest detects goroutines left blocked after the test body finishes. The PR routes mock clients through the scheduler; the base does not. The scheduler synchronization defect itself predates the PR.

This failure is scheduling-dependent. A passing run does not rule it out. Increase iterations if needed:

```bash
REPRO_ITERATIONS=5000 bash review-repros/pr-1427/run.sh
```

Each diagnostic has a 180-second real-time test timeout. Higher iteration counts may need a larger timeout in the runner. Repeated full `make test-race` runs can expose the shutdown issue, but this focused test avoids unrelated integrations.

## Share as a draft PR

Include only this directory in a draft PR targeting `feat/envelope-transport`; leave SDK source files unchanged. Suggested title: `test: reproduce envelope transport flush and shutdown regressions`. Suggested body:

> Diagnostic-only comparison of the envelope-transport PR against its immediate stack base. Run `bash review-repros/pr-1427/run.sh`. The base passes both diagnostics; the reviewed head exceeds the flush deadline and can leave its scheduler blocked during shutdown. No production code or workspace configuration changes are included. The shutdown reproduction is scheduling-dependent, and a nonzero runner exit is expected when a diagnostic fails.
