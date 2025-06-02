package sentry

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Global variable to prevent compiler optimizations from eliminating code.
var (
	benchGlobalResult bool
	benchGlobalEvents []*Event
)

type key int

const (
	ctxKey key = iota
)

func BenchmarkAsyncTransportSendEvent(b *testing.B) {
	// Report memory allocations
	b.ReportAllocs()

	if err := Init(ClientOptions{
		Dsn:              "https://3c3fd18b3fd44566aeab11385f391a48@o447951.ingest.us.sentry.io/5774600",
		EnableLogs:       true,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		HTTPClient: &http.Client{
			Transport: &mockRoundTripper{},
		},
	}); err != nil {
		b.Fatal("Failed to init the SDK")
	}

	// Create a random source to prevent predictable execution
	//nolint: gosec
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Use a unique seed for each iteration to prevent caching
		seed := rnd.Intn(10000)
		sendBenchEvents(seed)
		result := Flush(20 * time.Second)
		if !result {
			b.Fatal("Flush timeout")
		}
		// Store the result in the global variable to prevent dead code elimination
		benchGlobalResult = result

		// Force garbage collection occasionally to prevent reusing memory
		if i%10 == 0 {
			runtime.GC()
		}
	}

	CurrentHub().Client().Transport.Close()
}

func sendBenchEvents(seed int) {
	// Create temporary slice to prevent data from being optimized away
	events := make([]*Event, 0, 10)

	for i := 0; i < 10; i++ {
		// Create unique events for each iteration
		evt := &Event{
			// Use seed and iteration to create unique messages
			Message: fmt.Sprintf("benchmark event %d-%d", seed, i),
			Level:   LevelInfo,
			Sdk: SdkInfo{
				Name:    "benchmark",
				Version: "1.0.0",
			},
			Extra: map[string]interface{}{
				"iteration": i,
				"seed":      seed,
			},
		}
		events = append(events, evt)
		CaptureEvent(evt)
	}

	// Store events in global variable to prevent optimization
	benchGlobalEvents = events

	l := NewLogger(context.Background())
	for i := 0; i < 100; i++ {
		// Use unique message for each log
		l.Info(context.Background(), "benchmark "+strconv.Itoa(seed)+"-"+strconv.Itoa(i))

		ctx := context.WithValue(context.Background(), ctxKey, seed+i)
		tr := StartTransaction(ctx, "benchmark transaction "+strconv.Itoa(seed)+"-"+strconv.Itoa(i))

		// Add some random work to prevent optimizations
		//nolint: gosec
		time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)

		tr.Finish()
	}
}

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	// Add small random variation to simulate real network conditions
	//nolint: gosec
	jitter := time.Duration(rand.Intn(500)) * time.Microsecond
	time.Sleep(1*time.Millisecond + jitter)
	return &http.Response{
		StatusCode: 200,
		Body:       http.NoBody,
	}, nil
}
