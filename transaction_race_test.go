package sentry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestTransactionRaceConditions(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		testFn  func(_ *testing.T)
	}{
		{
			name:    "TransactionContext",
			timeout: 5 * time.Second,
			testFn:  testTransactionContextRace,
		},
		{
			name:    "SpanRecorder",
			timeout: 5 * time.Second,
			testFn:  testSpanRecorderRace,
		},
		{
			name:    "SpanFinish",
			timeout: 5 * time.Second,
			testFn:  testSpanFinishRace,
		},
		{
			name:    "SamplingDecision",
			timeout: 5 * time.Second,
			testFn:  testSamplingDecisionRace,
		},
		{
			name:    "PropagationContext",
			timeout: 5 * time.Second,
			testFn:  testPropagationContextRace,
		},
		{
			name:    "TransactionEvent",
			timeout: 10 * time.Second, // This one might need more time due to transport
			testFn:  testTransactionEventRace,
		},
		{
			name:    "ConcurrentSpanOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentSpanOperationsRace,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(_ *testing.T) {
			timeout := time.After(tc.timeout)
			done := make(chan bool)

			go func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Test %s panicked: %v", tc.name, r)
					}
					done <- true
				}()
				tc.testFn(t)
			}()

			select {
			case <-timeout:
				t.Fatalf("Test %s didn't finish in time (timeout: %v) - likely deadlock", tc.name, tc.timeout)
			case <-done:
				t.Logf("Test %s completed successfully", tc.name)
			}
		})
	}
}

func testTransactionContextRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 50

	var wg sync.WaitGroup

	// Goroutines creating transactions and passing them through context
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx := context.Background()
				transaction := StartSpan(ctx, fmt.Sprintf("tx-%d-%d", id, j))

				// Pass transaction through context to simulate real usage
				txCtx := transaction.Context()

				// Simulate nested operations that might access the transaction
				func(ctx context.Context) {
					if span, ok := ctx.Value(spanContextKey{}).(*Span); ok {
						span.SetTag("nested", "true")
					}
				}(txCtx)

				runtime.Gosched()
				transaction.Finish()
			}
		}(i)
	}

	// Goroutines creating child spans from context
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx := context.Background()
				parent := StartSpan(ctx, fmt.Sprintf("parent-%d-%d", id, j))
				parentCtx := parent.Context()

				// Create child from context
				child := StartSpan(parentCtx, fmt.Sprintf("child-%d-%d", id, j))
				child.SetData("parent_id", parent.SpanID)

				runtime.Gosched()
				child.Finish()
				parent.Finish()
			}
		}(i)
	}

	wg.Wait()
}

func testSpanRecorderRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 25

	ctx := context.Background()
	rootSpan := StartSpan(ctx, "root-transaction")
	defer rootSpan.Finish()

	var wg sync.WaitGroup

	// Goroutines creating child spans (all using same recorder)
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				child := rootSpan.StartChild(fmt.Sprintf("child-%d-%d", id, j))
				child.SetTag("worker", fmt.Sprintf("%d", id))
				runtime.Gosched()
				child.Finish()
			}
		}(i)
	}

	// Goroutines creating grandchild spans
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				child := rootSpan.StartChild(fmt.Sprintf("parent-%d-%d", id, j))
				grandchild := child.StartChild(fmt.Sprintf("grandchild-%d-%d", id, j))

				grandchild.SetData("depth", 2)
				runtime.Gosched()

				grandchild.Finish()
				child.Finish()
			}
		}(i)
	}

	wg.Wait()
}

func testSpanFinishRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 50

	var wg sync.WaitGroup

	// Test concurrent Finish() calls on the same span
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx := context.Background()
				span := StartSpan(ctx, fmt.Sprintf("span-%d-%d", id, j))

				// Try to finish the span multiple times concurrently
				var finishWg sync.WaitGroup
				for k := 0; k < 3; k++ {
					finishWg.Add(1)
					go func() {
						defer finishWg.Done()
						span.Finish() // Should only actually finish once
					}()
				}
				finishWg.Wait()
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testSamplingDecisionRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 50

	// Create a client with custom sampling
	client, _ := NewClient(ClientOptions{
		TracesSampleRate: 0.5,
		TracesSampler: func(_ SamplingContext) float64 {
			// Simulate some sampling logic that might have race conditions
			return 1.0
		},
	})

	hub := NewHub(client, NewScope())
	var wg sync.WaitGroup

	// Goroutines creating transactions with sampling decisions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx := SetHubOnContext(context.Background(), hub)
				transaction := StartSpan(ctx, fmt.Sprintf("sampled-tx-%d-%d", id, j))

				// Access sampling decision
				_ = transaction.Sampled

				// Add some data that might affect sampling
				transaction.SetData("worker_id", id)
				transaction.SetTag("iteration", fmt.Sprintf("%d", j))

				runtime.Gosched()
				transaction.Finish()
			}
		}(i)
	}

	wg.Wait()
}

func testPropagationContextRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 50

	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines accessing and modifying propagation context
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Get traceparent (reads propagation context)
				_ = hub.GetTraceparent()
				runtime.Gosched()
			}
		}()
	}

	// Goroutines getting baggage
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Get baggage (reads propagation context)
				_ = hub.GetBaggage()
				runtime.Gosched()
			}
		}()
	}

	// Goroutines modifying scope (which affects propagation context)
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.ConfigureScope(func(scope *Scope) {
					// This might modify propagation context
					ctx := context.Background()
					span := StartSpan(ctx, fmt.Sprintf("config-span-%d-%d", id, j))
					scope.SetSpan(span)
					span.Finish()
				})
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testTransactionEventRace(_ *testing.T) {
	const numGoroutines = 50
	const iterations = 25

	client, _ := NewClient(ClientOptions{
		TracesSampleRate: 1.0, // Sample all transactions
	})
	hub := NewHub(client, NewScope())
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines creating transactions that will generate events
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx := SetHubOnContext(context.Background(), hub)
				transaction := StartSpan(ctx, fmt.Sprintf("event-tx-%d-%d", id, j))

				// Add various data that will be included in the event
				transaction.SetTag("worker", fmt.Sprintf("%d", id))
				transaction.SetData("iteration", j)
				transaction.SetData("timestamp", time.Now())

				// Create child spans
				child1 := transaction.StartChild("child1")
				child2 := transaction.StartChild("child2")

				runtime.Gosched()

				child1.Finish()
				child2.Finish()
				transaction.Finish() // This will trigger event generation
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentSpanOperationsRace(_ *testing.T) {
	const numGoroutines = 100
	const iterations = 25

	ctx := context.Background()
	rootSpan := StartSpan(ctx, "concurrent-ops-root")
	defer rootSpan.Finish()

	var wg sync.WaitGroup

	// Goroutines performing different operations on child spans
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				span := rootSpan.StartChild(fmt.Sprintf("settag-span-%d-%d", id, j))
				span.SetTag("operation", "settag")
				span.SetTag("worker_id", fmt.Sprintf("%d", id))
				runtime.Gosched()
				span.Finish()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				span := rootSpan.StartChild(fmt.Sprintf("setdata-span-%d-%d", id, j))
				span.SetData("operation", "setdata")
				span.SetData("worker_id", id)
				span.SetData("complex_data", map[string]interface{}{
					"nested": map[string]string{"key": "value"},
					"array":  []int{1, 2, 3},
				})
				runtime.Gosched()
				span.Finish()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				span := rootSpan.StartChild(fmt.Sprintf("status-span-%d-%d", id, j))
				runtime.Gosched()
				span.Finish()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				span := rootSpan.StartChild(fmt.Sprintf("trace-span-%d-%d", id, j))
				// Access trace information
				_ = span.ToSentryTrace()
				_ = span.ToBaggage()
				_ = span.TraceID
				_ = span.SpanID
				runtime.Gosched()
				span.Finish()
			}
		}(i)
	}

	wg.Wait()
}
