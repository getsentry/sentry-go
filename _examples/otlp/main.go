// This example demonstrates two ways to set up OpenTelemetry tracing with Sentry.
//
// setupTracerProviderWithSentry exports spans directly to Sentry using
// sentryotlp.NewTraceExporter.
//
// setupTracerProviderWithCollector exports spans to a standard OpenTelemetry
// Collector using otlptracehttp.New.
//
// To link Sentry errors, register sentryotel.NewLinkingIntegration in
// sentry.Init.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryotel "github.com/getsentry/sentry-go/otel"
	sentryotlp "github.com/getsentry/sentry-go/otel/otlp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	dsn := os.Getenv("SENTRY_DSN")
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			return append(integrations, sentryotel.NewLinkingIntegration())
		},
	}); err != nil {
		log.Fatalf("sentry.Init: %v", err)
	}
	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	// Direct-to-Sentry setup:
	tp, err := setupTracerProviderWithSentry(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	// When exporting through a collector, keep the same Sentry initialization above
	// and switch only the TracerProvider setup:
	//
	// tp, err := setupTracerProviderWithCollector(ctx)
	// ...
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("TracerProvider.Shutdown: %v", err)
		}
	}()

	otel.SetTracerProvider(tp)

	mux := http.NewServeMux()
	mux.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("example-service").Start(r.Context(), "GET /demo")
		defer span.End()

		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub()
		}
		hub.Client().CaptureException(
			errors.New("demo handler failure"),
			&sentry.EventHint{Context: ctx},
			hub.Scope(),
		)

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("captured an error and linked it to the active trace\n"))
	})

	fmt.Println("Send a request to http://localhost:8080/demo to generate one trace and one linked error.")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// setupTracerProviderWithSentry sends spans directly to Sentry's OTLP endpoint.
func setupTracerProviderWithSentry(ctx context.Context, dsn string) (*sdktrace.TracerProvider, error) {
	exporter, err := sentryotlp.NewTraceExporter(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("sentryotlp.NewTraceExporter: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	), nil
}
