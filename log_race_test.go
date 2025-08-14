package sentry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/testutils"
)

const (
	loggingGoroutines = 50
	loggingIterations = 100
)

type CtxKey int

func TestLoggingRaceConditions(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		testFn  func(*testing.T)
	}{
		{
			name:    "ConcurrentLoggerSetAttributes",
			timeout: 5 * time.Second,
			testFn:  testConcurrentLoggerSetAttributes,
		},
		{
			name:    "ConcurrentLogEmission",
			timeout: 5 * time.Second,
			testFn:  testConcurrentLogEmission,
		},
		{
			name:    "ConcurrentLogEntryOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentLogEntryOperations,
		},
		{
			name:    "ConcurrentLoggerCreationAndUsage",
			timeout: testutils.FlushTimeout(),
			testFn:  testConcurrentLoggerCreationAndUsage,
		},
		{
			name:    "ConcurrentLogWithSpanOperations",
			timeout: 5 * time.Second,
			testFn:  testConcurrentLogWithSpanOperations,
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

func testConcurrentLoggerSetAttributes(t *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:        testDsn,
		EnableLogs: true,
		Transport:  &MockTransport{},
	})
	hub := NewHub(client, NewScope())
	ctx := SetHubOnContext(context.Background(), hub)

	logger := NewLogger(ctx)
	if _, ok := logger.(*noopLogger); ok {
		t.Skip("Logging is disabled, skipping test")
	}

	var wg sync.WaitGroup

	for i := 0; i < loggingGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loggingIterations; j++ {
				attrs := []attribute.Builder{
					attribute.String(fmt.Sprintf("attr-string-%d", id), fmt.Sprintf("value-%d-%d", id, j)),
					attribute.Int64(fmt.Sprintf("attr-int-%d", id), int64(id*j)),
					attribute.Float64(fmt.Sprintf("attr-float-%d", id), float64(id)+float64(j)*0.1),
					attribute.Bool(fmt.Sprintf("attr-bool-%d", id), j%2 == 0),
				}
				logger.SetAttributes(attrs...)
				runtime.Gosched()
			}
		}(i)
	}

	for i := 0; i < loggingGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loggingIterations; j++ {
				logger.Info().
					String("worker_id", fmt.Sprintf("%d", id)).
					Int("iteration", j).
					Emit("Concurrent log message from worker", id)
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentLogEmission(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:        testDsn,
		EnableLogs: true,
		Transport:  &MockTransport{},
	})
	hub := NewHub(client, NewScope())
	ctx := SetHubOnContext(context.Background(), hub)

	var wg sync.WaitGroup

	for i := 0; i < loggingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			logger := NewLogger(ctx)
			if _, ok := logger.(*noopLogger); ok {
				return
			}

			for j := 0; j < loggingIterations/5; j++ {
				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Trace().
						String("operation", "trace").
						Int("worker", id).
						Emit("Trace message from worker %d", id)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Debug().
						String("operation", "debug").
						Int("worker", id).
						Emit("Debug message from worker %d", id)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Info().
						String("operation", "info").
						Int("worker", id).
						Emit("Info message from worker %d", id)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Warn().
						String("operation", "warn").
						Int("worker", id).
						Emit("Warning message from worker %d", id)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Error().
						String("operation", "error").
						Int("worker", id).
						Emit("Error message from worker %d", id)
				}()

				localWg.Wait()
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentLogEntryOperations(t *testing.T) {
	t.Skip("A single instance of a log entry should not be used concurrently")

	client, _ := NewClient(ClientOptions{
		Dsn:        testDsn,
		EnableLogs: true,
		Transport:  &MockTransport{},
	})
	hub := NewHub(client, NewScope())
	ctx := SetHubOnContext(context.Background(), hub)

	logger := NewLogger(ctx)
	if _, ok := logger.(*noopLogger); ok {
		t.Skip("Logging is disabled, skipping test")
	}

	var wg sync.WaitGroup

	for i := 0; i < loggingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loggingIterations/10; j++ {
				entry := logger.Info()

				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					entry.String("worker_id", fmt.Sprintf("worker-%d", id))
					entry.Int("iteration", j)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					entry.Float64("progress", float64(j)/float64(loggingIterations/10))
					entry.Bool("is_test", true)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					newCtx := context.WithValue(ctx, CtxKey(2), fmt.Sprintf("test_value_%d", id))
					_ = entry.WithCtx(newCtx)
				}()

				localWg.Wait()
				entry.Emit("Concurrent entry operations test %d-%d", id, j)
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentLoggerCreationAndUsage(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:        testDsn,
		EnableLogs: true,
		Transport:  &MockTransport{},
	})
	hub := NewHub(client, NewScope())

	var wg sync.WaitGroup

	for i := 0; i < loggingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loggingIterations/20; j++ {
				ctx := context.WithValue(context.Background(), CtxKey(1), id)
				ctx = SetHubOnContext(ctx, hub)

				logger := NewLogger(ctx)
				if _, ok := logger.(*noopLogger); ok {
					continue
				}

				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.SetAttributes(
						attribute.String("creation_worker", fmt.Sprintf("%d", id)),
						attribute.Int64("creation_iteration", int64(j)),
					)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Info().
						String("immediate_usage", "true").
						Emit("Logger created and used immediately by worker %d", id)
				}()

				localWg.Wait()
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentLogWithSpanOperations(_ *testing.T) {
	client, _ := NewClient(ClientOptions{
		Dsn:              testDsn,
		EnableLogs:       true,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        &MockTransport{},
	})
	hub := NewHub(client, NewScope())
	ctx := SetHubOnContext(context.Background(), hub)

	var wg sync.WaitGroup

	for i := 0; i < loggingGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loggingIterations/20; j++ {
				transaction := StartTransaction(ctx, fmt.Sprintf("log-transaction-%d", id))
				span := transaction.StartChild(fmt.Sprintf("log-span-%d", id))

				spanCtx := span.Context()
				logger := NewLogger(spanCtx)
				if _, ok := logger.(*noopLogger); ok {
					span.Finish()
					transaction.Finish()
					continue
				}

				var localWg sync.WaitGroup

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					span.SetTag("worker_id", fmt.Sprintf("%d", id))
					span.SetData("iteration", j)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.SetAttributes(
						attribute.String("span_operation", span.Op),
						attribute.String("trace_id", span.TraceID.String()),
					)
				}()

				localWg.Add(1)
				go func() {
					defer localWg.Done()
					logger.Info().
						String("span_context", "active").
						String("span_id", span.SpanID.String()).
						Emit("Log within span from worker %d", id)
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
