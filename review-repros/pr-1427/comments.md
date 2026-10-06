# Suggested review comments for PR #1427

Post these on [PR #1427](https://github.com/getsentry/sentry-go/pull/1427), not on the diagnostic draft PR. Locations below refer to [reviewed commit 4fd76f1](https://github.com/getsentry/sentry-go/commit/4fd76f13b03771b0e3daf4e0b0a965a971b99b57) and are added lines in its diff. Attach the draft reproduction PR link after creating it.

## 1. Flush deadline regression

**Location:** `client.go`, right-side line 758: `return client.telemetryProcessor.FlushWithContext(ctx)`. The [client flush change](https://github.com/getsentry/sentry-go/blob/4fd76f1/client.go#L754-L759) makes the scheduler handle flushing for synchronous transports too.

**Paste-ready comment:**

> Routing the synchronous transport through the processor appears to break the flush deadline for buffered logs/metrics. With one buffered log and a fake HTTP request that stalls for 30 seconds, `client.Flush(time.Second)` takes 30 seconds and returns `true`; the equivalent stack-base run returns after one second.
>
> `Scheduler.FlushWithContext` calls `flushBuffers()` synchronously before reaching the transport flush. That drain invokes blocking `SyncTransport.SendEnvelope`, which uses a background context rather than the flush context. Can we make the deadline cover buffer draining/submission as well, not just the final transport flush?
>
> Both versions already return `true` in this scenario; the newly introduced regression is the caller waiting beyond its deadline.

The [comparison runner](run.sh) and [shared diagnostic tests](repro_test.go) reproduce the timing difference with simulated time and no network requests.

## 2. Scheduler shutdown defect exposed to mock/custom clients

**Location:** `client.go`, right-side line 423: `client.setupTelemetryProcessor()`. The [unconditional processor initialization](https://github.com/getsentry/sentry-go/blob/4fd76f1/client.go#L422-L423) expands the affected client configurations; the synchronization defect is in existing scheduler code.

**Paste-ready comment:**

> Starting the processor for mock/custom transports exposes an existing scheduler shutdown race. Repeatedly creating a client with `MockTransport` and immediately closing it inside `testing/synctest` passes on the stack base, but fails on this head with a deadlock: the scheduler is left blocked in `sync.Cond.Wait` after `Close` returns.
>
> `Scheduler.Stop` cancels and broadcasts without holding `s.mu`. The worker can check cancellation, then miss the broadcast before entering `Wait`; the periodic notifier also exits on cancellation, so no later notification is guaranteed. Can we synchronize cancellation/notification with the waiter before enabling this path for every client?
>
> This is scheduling-dependent, but the focused 1,000-iteration reproduction has failed repeatedly. The same deadlock also appeared in a full workspace race-test run.

Inspect the [existing shutdown sequence](https://github.com/getsentry/sentry-go/blob/4fd76f1/internal/telemetry/scheduler.go#L113-L129) alongside the [wait loop](https://github.com/getsentry/sentry-go/blob/4fd76f1/internal/telemetry/scheduler.go#L190-L194).

## Optional clarification: category prioritization

This is a design question, not a confirmed additional bug. Post only if the priority/overflow change is not already explained elsewhere in the stack.

**Location:** `internal/telemetry/processor.go`, right-side line 47: `return b.scheduler.sendItem(convertible)`, in the [inline submission change](https://github.com/getsentry/sentry-go/blob/4fd76f1/internal/telemetry/processor.go#L38-L50).

**Paste-ready comment:**

> Is bypassing category buffers and scheduler prioritization intentional for the default async transport too? I understand why inline submission is needed to make synchronous error capture block, but errors, transactions, and check-ins now go directly to the shared transport queue for every transport. That also changes overload behavior from category-buffer eviction to rejecting the incoming envelope when the transport queue is full. Could you clarify the intended priority/retention policy? The corresponding category buffers still appear to be allocated but no longer receive these captures.

I would not post a generic comment about mock decoding without identifying a specific incorrect assertion or lost field. Those changes are a manual-review focus, not a demonstrated defect.
