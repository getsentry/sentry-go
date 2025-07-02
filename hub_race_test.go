package sentry

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestHubRaceConditions(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		testFn  func(_ *testing.T)
	}{
		{
			name:    "HubStackManipulation",
			timeout: 5 * time.Second,
			testFn:  testHubStackManipulationRace,
		},
		{
			name:    "ScopeFieldAccess",
			timeout: 5 * time.Second,
			testFn:  testScopeFieldAccessRace,
		},
		{
			name:    "HubWithScope",
			timeout: 5 * time.Second,
			testFn:  testHubWithScopeRace,
		},
		{
			name:    "EventCapture",
			timeout: 5 * time.Second,
			testFn:  testEventCaptureRace,
		},
		{
			name:    "HubClone",
			timeout: 5 * time.Second,
			testFn:  testHubCloneRace,
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

func testHubStackManipulationRace(_ *testing.T) {
	const numGoroutines = 40
	const iterations = 20

	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines pushing and popping scopes
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				scope := hub.PushScope()
				scope.SetTag("pusher", fmt.Sprintf("%d-%d", id, j))
				runtime.Gosched()
				hub.PopScope()
			}
		}(i)
	}

	// Goroutines reading from hub
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = hub.Client()
				scope := hub.Scope()
				scope.SetTag("reader", fmt.Sprintf("%d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines binding new clients
	for i := 0; i < numGoroutines/3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				newClient, _ := NewClient(ClientOptions{})
				hub.BindClient(newClient)
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
}

func testScopeFieldAccessRace(_ *testing.T) {
	const numGoroutines = 40
	const iterations = 20

	scope := NewScope()
	var wg sync.WaitGroup

	// Goroutines modifying tags
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				scope.SetTag(fmt.Sprintf("key-%d", id), fmt.Sprintf("value-%d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines modifying contexts
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				scope.SetContext(fmt.Sprintf("ctx-%d", id), map[string]interface{}{
					"value": fmt.Sprintf("%d-%d", id, j),
				})
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines adding breadcrumbs
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				scope.AddBreadcrumb(&Breadcrumb{
					Message: fmt.Sprintf("breadcrumb-%d-%d", id, j),
				}, 100)
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines cloning scope
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = scope.Clone()
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
}

func testHubWithScopeRace(_ *testing.T) {
	const numGoroutines = 40
	const iterations = 20

	hub, _, _ := setupHubTest()

	var wg sync.WaitGroup

	// Goroutines using WithScope
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.WithScope(func(scope *Scope) {
					scope.SetTag("withscope", fmt.Sprintf("%d-%d", id, j))
					scope.SetLevel(LevelInfo)
				})
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines using ConfigureScope
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.ConfigureScope(func(scope *Scope) {
					scope.SetTag("configurescope", fmt.Sprintf("%d-%d", id, j))
					scope.SetContext("worker", map[string]interface{}{
						"id":        id,
						"iteration": j,
					})
				})
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testEventCaptureRace(_ *testing.T) {
	const numGoroutines = 40
	const iterations = 20

	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines capturing different types of events
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.CaptureMessage(fmt.Sprintf("message %d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.CaptureException(fmt.Errorf("error %d-%d", id, j))
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines modifying hub state
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				scope := hub.PushScope()
				scope.SetTag("capture-race", fmt.Sprintf("%d-%d", id, j))
				runtime.Gosched()
				hub.PopScope()
			}
		}(i)
	}

	// Goroutines adding breadcrumbs during capture
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.AddBreadcrumb(&Breadcrumb{
					Message: fmt.Sprintf("breadcrumb %d-%d", id, j),
					Level:   LevelInfo,
				}, nil)
				runtime.Gosched()
			}
		}(i)
	}

	wg.Wait()
}

func testHubCloneRace(_ *testing.T) {
	const numGoroutines = 40
	const iterations = 20

	hub, client, _ := setupHubTest()
	transport := &MockTransport{}
	client.Transport = transport

	var wg sync.WaitGroup

	// Goroutines cloning hub
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				clone := hub.Clone()
				clone.ConfigureScope(func(scope *Scope) {
					scope.SetTag("clone", fmt.Sprintf("%d-%d", id, j))
				})
				runtime.Gosched()
			}
		}(i)
	}

	// Goroutines modifying original hub
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hub.ConfigureScope(func(scope *Scope) {
					scope.SetTag("original", fmt.Sprintf("%d-%d", id, j))
				})
				scope := hub.PushScope()
				scope.SetLevel(LevelWarning)
				runtime.Gosched()
				hub.PopScope()
			}
		}(i)
	}

	wg.Wait()
}
