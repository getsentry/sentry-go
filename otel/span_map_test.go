package sentryotel

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type noopExporter struct{}

func (e *noopExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error {
	return nil
}
func (e *noopExporter) Shutdown(_ context.Context) error { return nil }

func setupTracerProvider(useSentry bool) (*sdktrace.TracerProvider, func()) {
	res, _ := resource.New(context.Background())

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	if useSentry {
		sentryProcessor := NewSentrySpanProcessor()
		tp.RegisterSpanProcessor(sentryProcessor)
	}

	tp.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(&noopExporter{}))

	otel.SetTracerProvider(tp)
	return tp, func() { _ = tp.Shutdown(context.Background()) }
}

func simulateWorkflowBatch(ctx context.Context, tracer trace.Tracer, numInstances, dbSpansPerInstance int) {
	ctx, rootSpan := tracer.Start(ctx, "job.workflow_runner")

	var wg sync.WaitGroup
	for i := 0; i < numInstances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			wfCtx, wfSpan := tracer.Start(ctx, fmt.Sprintf("workflow.debt_reminder_%d", idx))

			for j := 0; j < dbSpansPerInstance; j++ {
				_, dbSpan := tracer.Start(wfCtx, fmt.Sprintf("postgres.query_%d", j))
				dbSpan.End()
			}

			wfSpan.End()
		}(i)
	}
	wg.Wait()
	rootSpan.End()
}

// BenchmarkSpanMapContention measures how much the Sentry span processor slows down unrelated handler
// spans when a large workflow transaction is being created and cleaned up concurrently.
func BenchmarkSpanMapContention(b *testing.B) {
	_, cleanup := setupTracerProvider(true)
	defer cleanup()
	tracer := otel.Tracer("bench")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		iter := 0
		for pb.Next() {
			if iter%500 == 0 {
				// Every 500th iteration, simulate a large workflow batch.
				// This creates 100×33 = 3300 child spans under a single root,
				// then ends them all — exercising the cleanup path under contention.
				simulateWorkflowBatch(context.Background(), tracer, 100, 33)
			} else {
				// This is the hot path that gets blocked from span cleanup.
				ctx, span := tracer.Start(context.Background(), "GET /api/ping")
				_, child := tracer.Start(ctx, "db.query")
				child.End()
				span.End()
			}
			iter++
		}
	})
}
