package sentrytest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/getsentry/sentry-go"
)

// CheckRequestIsolation exercises sequential reuse and concurrent requests.
// request returns the completed request context and, optionally, its outer
// middleware context. Both contexts must be canceled when the request ends.
func CheckRequestIsolation(t *testing.T, request func() (context.Context, context.Context, error)) {
	t.Helper()
	const concurrentRequests = 32
	type result struct{ inner, outer context.Context }
	results := make(chan result, concurrentRequests+2)
	run := func() {
		inner, outer, err := request()
		if err != nil {
			t.Error(err)
			return
		}
		results <- result{inner, outer}
	}
	for range 2 {
		run()
	}
	var wg sync.WaitGroup
	for range concurrentRequests {
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}
	wg.Wait()
	close(results)

	scopes := make(map[*sentry.Scope]bool)
	traces := make(map[sentry.TraceID]bool)
	for result := range results {
		if result.inner == nil {
			t.Fatal("request returned a nil context")
		}
		if !errors.Is(result.inner.Err(), context.Canceled) {
			t.Fatalf("request context error = %v, want context canceled", result.inner.Err())
		}
		if result.outer != nil && !errors.Is(result.outer.Err(), context.Canceled) {
			t.Fatalf("outer request context error = %v, want context canceled", result.outer.Err())
		}
		scope := sentry.ScopeFromContext(result.inner)
		span := sentry.SpanFromContext(result.inner)
		if scope == nil || span == nil {
			t.Fatal("request context is missing its scope or span")
		}
		if scopes[scope] {
			t.Fatal("request reused an isolation scope")
		}
		if traces[span.TraceID] {
			t.Fatal("request reused a trace ID")
		}
		scopes[scope], traces[span.TraceID] = true, true
	}
	if len(scopes) != concurrentRequests+2 {
		t.Errorf("isolated requests = %d, want %d", len(scopes), concurrentRequests+2)
	}
}
