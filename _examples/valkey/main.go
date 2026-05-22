// This is an example program that demonstrates Sentry instrumentation for
// valkey-go. It shows both cache mode (default) and database query mode.
//
// Try it by running:
//
//	go run main.go
//
// Requires a running Valkey/Redis instance on localhost:6379.
// Set VALKEY_ADDR to override.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryvalkey "github.com/getsentry/sentry-go/valkey"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyhook"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn:              "",
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			fmt.Printf("Transaction: %s (%d spans)\n", event.Transaction, len(event.Spans))
			for _, span := range event.Spans {
				fmt.Printf("  [%s] %s\n", span.Op, span.Description)
			}
			return event
		},
		Debug: true,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	defer sentry.Flush(2 * time.Second)

	addr := "localhost:6379"
	if v := os.Getenv("VALKEY_ADDR"); v != "" {
		addr = v
	}

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
	})
	if err != nil {
		log.Fatalf("valkey.NewClient: %s", err)
	}
	defer client.Close()

	// Cache mode (default): reports cache.get / cache.put spans.
	cacheHook := sentryvalkey.New(sentryvalkey.Options{})
	cacheClient := valkeyhook.WithHook(client, cacheHook)
	cacheExample(cacheClient)

	// DB mode: reports db.query spans with scrubbed command descriptions.
	dbHook := sentryvalkey.New(sentryvalkey.Options{Type: sentryvalkey.TypeDB})
	dbClient := valkeyhook.WithHook(client, dbHook)
	dbExample(dbClient)
}

func cacheExample(client valkey.Client) {
	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone())
	tx := sentry.StartTransaction(ctx, "valkey-cache-example")
	defer tx.Finish()

	ctx = tx.Context()

	// cache.put
	client.Do(ctx, client.B().Set().Key("user:123").Value("Alice").Ex(60).Build())

	// cache.get (hit)
	result := client.Do(ctx, client.B().Get().Key("user:123").Build())
	val, _ := result.ToString()
	fmt.Printf("Cache hit: %s\n", val)

	// cache.get (miss)
	client.Do(ctx, client.B().Get().Key("nonexistent:key").Build())

	// cache.remove
	client.Do(ctx, client.B().Del().Key("user:123").Build())
}

func dbExample(client valkey.Client) {
	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone())
	tx := sentry.StartTransaction(ctx, "valkey-db-example")
	defer tx.Finish()

	ctx = tx.Context()

	// db.query — values scrubbed in span description
	client.Do(ctx, client.B().Set().Key("session:abc").Value("secret-token").Build())
	client.Do(ctx, client.B().Get().Key("session:abc").Build())

	// db.query.pipeline
	client.DoMulti(ctx,
		client.B().Set().Key("counter:a").Value("1").Build(),
		client.B().Set().Key("counter:b").Value("2").Build(),
		client.B().Get().Key("counter:a").Build(),
	)
}
