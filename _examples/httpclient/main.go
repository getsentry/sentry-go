package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              "",
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			fmt.Println(event)
			return event
		},
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			fmt.Println(event)
			return event
		},
		Debug: true,
	})

	// With custom HTTP client
	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone())
	httpClient := &http.Client{
		Transport: sentryhttpclient.NewSentryRoundTripper(nil),
	}

	err := getExamplePage(ctx, httpClient)
	if err != nil {
		panic(err)
	}
}

func getExamplePage(ctx context.Context, httpClient *http.Client) error {
	span := sentry.StartSpan(ctx, "getExamplePage")
	ctx = span.Context()
	defer span.Finish()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		if response.Body != nil {
			_ = response.Body.Close()
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	fmt.Println(string(body))

	return nil
}
