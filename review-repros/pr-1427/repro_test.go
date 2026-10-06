package prreview

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	sentry "github.com/getsentry/sentry-go"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFlushDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := sentry.NewClient(sentry.ClientOptions{
			Dsn:                  "https://key@example.com/1",
			Transport:            newSyncTransport(),
			DisableClientReports: true,
			HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				// Identical simulation for both APIs; no network request is made.
				// The timer also allows the legacy background sender to terminate.
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-time.After(30 * time.Second):
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
				}
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		ctx := sentry.ContextWithClient(context.Background(), client)
		sentry.NewLogger(ctx).Info().Emit("buffered log")

		start := time.Now()
		ok := client.Flush(time.Second)
		elapsed := time.Since(start)
		t.Logf("Flush(1s): elapsed=%v result=%v", elapsed, ok)
		// Only assert the deadline: the base already incorrectly returns true.
		if elapsed > time.Second {
			t.Errorf("flush exceeded its deadline: elapsed=%v", elapsed)
		}
	})
}

func TestImmediateClose(t *testing.T) {
	iterations := 1000
	if value := os.Getenv("REPRO_ITERATIONS"); value != "" {
		var err error
		iterations, err = strconv.Atoi(value)
		if err != nil || iterations < 1 {
			t.Fatalf("invalid REPRO_ITERATIONS: %q", value)
		}
	}
	for i := 0; i < iterations; i++ {
		synctest.Test(t, func(t *testing.T) {
			client, err := sentry.NewClient(sentry.ClientOptions{
				Transport: &sentry.MockTransport{},
			})
			if err != nil {
				t.Fatal(err)
			}
			client.Close()
		})
	}
	t.Logf("completed %d immediate client closes", iterations)
}
