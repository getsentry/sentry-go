package sentry

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func BenchmarkAsyncTransportSendEvent(b *testing.B) {
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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sendBenchEvents()
		if ok := Flush(20 * time.Second); !ok {
			b.Fatal("Flush timeout")
		}
	}
	CurrentHub().Client().Transport.Close()
}

func sendBenchEvents() {
	for i := 0; i < 10; i++ {
		CaptureEvent(&Event{
			Message: "benchmark event",
			Level:   LevelInfo,
			Sdk: SdkInfo{
				Name:    "benchmark",
				Version: "1.0.0",
			},
		})
	}

	l := NewLogger(context.Background())
	for i := 0; i < 100; i++ {
		l.Info(context.Background(), "benchmark")
		tr := StartTransaction(context.Background(), "benchmark transaction")
		tr.Finish()
	}
}

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	// simulate fast HTTP response
	time.Sleep(1 * time.Millisecond)
	return &http.Response{
		StatusCode: 200,
		Body:       http.NoBody,
	}, nil
}
