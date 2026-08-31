package main

import (
	"context"
	"fmt"
	"log"

	"github.com/getsentry/sentry-go"
)

type pickleIntegration struct{}

func (pi *pickleIntegration) Name() string {
	return "PickleIntegration"
}

func (pi *pickleIntegration) SetupOnce(client *sentry.Client) {
	client.AddEventProcessor(pi.processor)
}

func (pi *pickleIntegration) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	event.Message = fmt.Sprintf("PickleRick Says: %s", event.Message)
	return event
}

func main() {
	client1, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://hello@example.com/1",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			log.Println(event.Message)
			return nil
		},
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			return append(integrations, &pickleIntegration{})
		},
	})
	ctx1, scope1 := sentry.WithIsolationScope(context.Background())
	scope1.SetClient(client1)

	client2, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://hello@example.com/2",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			log.Println(event.Message)
			return nil
		},
	})
	ctx2, scope2 := sentry.WithIsolationScope(context.Background())
	scope2.SetClient(client2)

	sentry.CaptureMessage(ctx1, "Client 1: altered message by pickleIntegration")
	sentry.CaptureMessage(ctx2, "Client 2: _NOT_ altered message by pickleIntegration")
}
