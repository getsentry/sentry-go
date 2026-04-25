// This is an example program that demonstrates Sentry instrumentation for
// rueidis. It shows both cache mode (default) and database query mode.
//
// Try it by running:
//
//	go run main.go
//
// Requires a running Redis instance on localhost:6379.
// Set REDIS_ADDR to override.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryrueidis "github.com/getsentry/sentry-go/rueidis"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidishook"
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
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		addr = v
	}

	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{addr},
	})
	if err != nil {
		log.Fatalf("rueidis.NewClient: %s", err)
	}
	defer client.Close()

	// Cache mode (default): reports cache.get / cache.put spans.
	cacheHook := sentryrueidis.New(sentryrueidis.Options{})
	cacheClient := rueidishook.WithHook(client, cacheHook)
	cacheExample(cacheClient)

	// DB mode: reports db.query spans with scrubbed command descriptions.
	dbHook := sentryrueidis.New(sentryrueidis.Options{Type: sentryrueidis.TypeDB})
	dbClient := rueidishook.WithHook(client, dbHook)
	dbExample(dbClient)
}

func cacheExample(client rueidis.Client) {
	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone())
	tx := sentry.StartTransaction(ctx, "rueidis-cache-example")
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

func dbExample(client rueidis.Client) {
	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone())
	tx := sentry.StartTransaction(ctx, "rueidis-db-example")
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
