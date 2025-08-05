package sentry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

const (
	tracingGoroutines = 50
	tracingIterations = 100
)

func TestTracingRaceConditions(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		testFn  func(*testing.T)
	}{
		{
			name:    "ConcurrentSpanSetOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentSpanSetOperations,
		},
		{
			name:    "ConcurrentTransactionOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentTransactionOperations,
		},
		{
			name:    "ConcurrentTraceAndBaggageGeneration",
			timeout: 5 * time.Second,
			testFn:  testConcurrentTraceAndBaggageGeneration,
		},
		{
			name:    "ConcurrentSpanFinishWithSetOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentSpanFinishWithSetOperations,
		},
		{
			name:    "ConcurrentChildSpanCreation",
			timeout: 5 * time.Second,
			testFn:  testConcurrentChildSpanCreation,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

func testConcurrentSpanSetOperations(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	hub := NewHub(client, NewScope())

	ctx := SetHubOnContext(context.Background(), hub)
	transaction := StartTransaction(ctx, "test-transaction")
	defer transaction.Finish()

	var wg sync.WaitGroup

	for i := 0; i < tracingGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations; j++ {
				transaction.SetTag(fmt.Sprintf("tag-%d", id), fmt.Sprintf("value-%d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	for i := 0; i < tracingGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations; j++ {
				transaction.SetData(fmt.Sprintf("data-%d", id), map[string]interface{}{
					"worker": id,
					"iter":   j,
					"time":   time.Now().Unix(),
				})
				runtime.Gosched()
			}
		}(i)
	}

	for i := 0; i < tracingGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations; j++ {
				transaction.SetContext(fmt.Sprintf("context-%d", id), map[string]interface{}{
					"worker_id": id,
					"iteration": j,
				})
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentTransactionOperations(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	hub := NewHub(client, NewScope())

	ctx := SetHubOnContext(context.Background(), hub)

	var wg sync.WaitGroup

	for i := 0; i < tracingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations/10; j++ {
				transaction := StartTransaction(ctx, fmt.Sprintf("transaction-%d-%d", id, j))

				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					transaction.SetTag("worker", fmt.Sprintf("%d", id))
					transaction.SetData("metadata", map[string]interface{}{"id": id, "iter": j})
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					_ = transaction.ToSentryTrace()
					_ = transaction.ToBaggage()
				}()

				localWg.Wait()
				transaction.Finish()
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentTraceAndBaggageGeneration(t *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	hub := NewHub(client, NewScope())

	ctx := SetHubOnContext(context.Background(), hub)
	transaction := StartTransaction(ctx, "test-trace-baggage")
	defer transaction.Finish()

	var wg sync.WaitGroup
	traces := make([]string, tracingGoroutines)
	baggages := make([]string, tracingGoroutines)

	for i := 0; i < tracingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations/5; j++ {
				traces[id] = transaction.ToSentryTrace()
				baggages[id] = transaction.ToBaggage()

				transaction.SetTag(fmt.Sprintf("concurrent-tag-%d", id), fmt.Sprintf("value-%d", j))
				transaction.SetData(fmt.Sprintf("concurrent-data-%d", id), j)

				runtime.Gosched()
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < tracingIterations; i++ {
			dsc := DynamicSamplingContext{
				Entries: map[string]string{
					"sample_rate": "1.0",
					"public_key":  "test-key",
				},
				Frozen: true,
			}
			transaction.SetDynamicSamplingContext(dsc)
			runtime.Gosched()
		}
	}()

	wg.Wait()

	for i, trace := range traces {
		if trace == "" {
			t.Errorf("Empty trace generated for goroutine %d", i)
		}
	}
}

func testConcurrentSpanFinishWithSetOperations(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	hub := NewHub(client, NewScope())

	ctx := SetHubOnContext(context.Background(), hub)

	var wg sync.WaitGroup

	for i := 0; i < tracingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tracingIterations/20; j++ {
				transaction := StartTransaction(ctx, fmt.Sprintf("concurrent-finish-%d-%d", id, j))
				span := transaction.StartChild(fmt.Sprintf("child-span-%d", id))

				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					span.SetTag("operation", "concurrent-test")
					span.SetData("worker_id", id)
					span.SetData("iteration", j)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					_ = span.ToSentryTrace()
					_ = transaction.ToBaggage()
				}()

				localWg.Wait()
				span.Finish()
				transaction.Finish()
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentChildSpanCreation(t *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	hub := NewHub(client, NewScope())

	ctx := SetHubOnContext(context.Background(), hub)
	transaction := StartTransaction(ctx, "test-child-spans")
	defer transaction.Finish()

	var wg sync.WaitGroup
	spans := make([]*Span, tracingGoroutines)

	for i := 0; i < tracingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			span := transaction.StartChild(fmt.Sprintf("child-%d", id))
			spans[id] = span

			var localWg sync.WaitGroup

			localWg.Add(1)
			go func() {
				defer localWg.Done()
				for j := 0; j < tracingIterations/10; j++ {
					span.SetTag(fmt.Sprintf("child-tag-%d", j), fmt.Sprintf("value-%d", id))
					runtime.Gosched()
				}
			}()

			localWg.Add(1)
			go func() {
				defer localWg.Done()
				for j := 0; j < tracingIterations/10; j++ {
					span.SetData(fmt.Sprintf("child-data-%d", j), map[string]interface{}{
						"parent_id": id,
						"value":     j,
					})
					runtime.Gosched()
				}
			}()

			localWg.Add(1)
			go func() {
				defer localWg.Done()
				for j := 0; j < tracingIterations/10; j++ {
					_ = span.ToSentryTrace()
					runtime.Gosched()
				}
			}()

			localWg.Wait()
			span.Finish()
		}(i)
	}

	wg.Wait()

	for i, span := range spans {
		if span == nil {
			t.Errorf("Span %d was not created", i)
		}
	}
}
