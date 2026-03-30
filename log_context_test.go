package sentry

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
)

func TestSentryLogger_ShouldLinkToCorrectSpan(t *testing.T) {
	transport := &MockTransport{}
	err := Init(ClientOptions{
		Dsn:              testDsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		EnableLogs:       true,
		Transport:        transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Flush(testutils.FlushTimeout())

	hub := CurrentHub().Clone()
	ctx1 := SetHubOnContext(context.Background(), hub)
	span1 := StartTransaction(ctx1, "request-1")
	ctx1 = span1.Context()
	traceID1 := span1.TraceID

	logger := NewLogger(ctx1)

	logger.Info().Emit("Log in request-1")
	span1.Finish()

	ctx2 := SetHubOnContext(context.Background(), hub)
	span2 := StartTransaction(ctx2, "request-2")
	ctx2 = span2.Context()
	traceID2 := span2.TraceID

	logger.Info().Emit("Log from request-1 logger during request-2")
	logger.Info().WithCtx(ctx2).Emit("Explicit override to request-2")
	span2.Finish()

	Flush(testutils.FlushTimeout())
	events := transport.Events()
	var logs []struct {
		message string
		traceID TraceID
	}

	for _, event := range events {
		for _, log := range event.Logs {
			logs = append(logs, struct {
				message string
				traceID TraceID
			}{
				message: log.Body,
				traceID: log.TraceID,
			})
		}
	}

	if len(logs) != 3 {
		t.Fatalf("Expected 3 logs, got %d", len(logs))
	}

	if logs[0].traceID != traceID1 {
		t.Errorf("Log 1: expected TraceID %s (request-1), got %s", traceID1, logs[0].traceID)
	}
	if logs[1].traceID != traceID1 {
		t.Errorf("Log 2: expected TraceID %s (request-1), got %s", traceID1, logs[1].traceID)
	}
	if logs[2].traceID != traceID2 {
		t.Errorf("Log 3: expected TraceID %s (request-2), got %s", traceID2, logs[2].traceID)
	}
}
