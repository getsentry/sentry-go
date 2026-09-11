package sentry_test

import (
	"context"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/protocol"
)

type loggingEnvelopeTransport struct{ sentry.Transport }

func (t *loggingEnvelopeTransport) SendEnvelope(envelope *protocol.Envelope) error {
	log.Printf("Sending envelope with %d items", len(envelope.Items))
	return t.Transport.SendEnvelope(envelope)
}

func ExampleNewHTTPTransport() {
	const dsn = "https://public@example.com/1"
	transport := &loggingEnvelopeTransport{Transport: sentry.NewHTTPTransport(sentry.TransportOptions{Dsn: dsn})}
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: dsn, Transport: transport})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	defer client.Flush(2 * time.Second)
	ctx, _ := sentry.WithIsolationScope(context.Background())
	ctx = sentry.ContextWithClient(ctx, client)
	sentry.CaptureMessage(ctx, "wrapped transport")
}
