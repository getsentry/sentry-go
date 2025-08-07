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
	numGoroutines = 50
	iterations    = 100
)

func TestHubRaceConditions(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		testFn  func(*testing.T)
	}{
		{
			name:    "HubCloneWithStackModification",
			timeout: 5 * time.Second,
			testFn:  testHubCloneRace,
		},
		{
			name:    "SpanFinishWithHubAccess",
			timeout: 5 * time.Second,
			testFn:  testSpanFinishHubRace,
		},
		{
			name:    "ConcurrentGoroutineIsolation",
			timeout: 5 * time.Second,
			testFn:  testConcurrentGoroutineIsolation,
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

func testHubCloneRace(_ *testing.T) {
	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines cloning hub while others modify the stack
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				localHub := hub.Clone()
				localHub.ConfigureScope(func(scope *Scope) {
					scope.SetTag("worker_id", fmt.Sprintf("%d", id))
					scope.SetExtra("iteration", j)
				})
				scope1 := localHub.PushScope()
				scope1.SetTag("level1", fmt.Sprintf("%d-%d", id, j))
				scope2 := localHub.PushScope()
				scope2.SetTag("level2", fmt.Sprintf("%d-%d", id, j))
				localHub.PopScope()
				localHub.PopScope()

				localHub.CaptureMessage(fmt.Sprintf("Message from worker %d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.WithScope(func(scope *Scope) {
					scope.SetTag("middleware", fmt.Sprintf("handler-%d", id))
					scope.SetLevel(LevelInfo)
				})
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testSpanFinishHubRace(_ *testing.T) {
	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport
	client.options.EnableTracing = true
	client.options.TracesSampleRate = 1.0

	ctx := context.Background()
	ctx = SetHubOnContext(ctx, hub)

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				transaction := StartTransaction(ctx, fmt.Sprintf("operation-%d", id))

				childSpan := transaction.StartChild(fmt.Sprintf("child-%d-%d", id, j))
				childSpan.SetTag("worker", fmt.Sprintf("%d", id))

				runtime.Gosched()
				childSpan.Finish()
				transaction.Finish()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.WithScope(func(scope *Scope) {
					scope.SetTag("concurrent_mod", fmt.Sprintf("%d-%d", id, j))
					scope.SetUser(User{ID: fmt.Sprintf("user-%d", id)})
				})
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentGoroutineIsolation(_ *testing.T) {
	const numWorkers = 20
	const tasksPerWorker = 30

	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerHub := hub.Clone()
			workerHub.ConfigureScope(func(scope *Scope) {
				scope.SetTag("worker_id", fmt.Sprintf("worker-%d", workerID))
				scope.SetUser(User{ID: fmt.Sprintf("worker-%d", workerID)})
			})

			for taskID := 0; taskID < tasksPerWorker; taskID++ {
				workerHub.WithScope(func(scope *Scope) {
					scope.SetTag("task_id", fmt.Sprintf("task-%d", taskID))
					scope.SetExtra("worker_data", map[string]interface{}{
						"worker": workerID,
						"task":   taskID,
					})

					if taskID%7 == 0 {
						workerHub.CaptureException(fmt.Errorf("task %d failed in worker %d", taskID, workerID))
					} else {
						workerHub.CaptureMessage(fmt.Sprintf("Task %d completed by worker %d", taskID, workerID))
					}
				})

				runtime.Gosched()
			}
		}(i)
	}

	go func() {
		for i := 0; i < 100; i++ {
			hub.ConfigureScope(func(scope *Scope) {
				scope.SetTag("main_thread_op", fmt.Sprintf("op-%d", i))
			})
			runtime.Gosched()
		}
	}()

	wg.Wait()
}
