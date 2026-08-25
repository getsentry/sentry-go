package main

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	sentryfasthttp "github.com/getsentry/sentry-go/fasthttp"
	"github.com/valyala/fasthttp"
)

func enhanceSentryEvent(handler fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		sentryfasthttp.GetScopeFromContext(ctx).SetTag("someRandomTag", "maybeYouNeedIt")

		expensiveThing := func(ctx context.Context) error {
			span := sentry.StartTransaction(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
			return nil
		}

		// Acquire the transaction from the stored request context. It may be nil
		// if you did not set up the sentryfasthttp middleware.
		sentrySpan := sentryfasthttp.GetSpanFromContext(ctx)
		// Pass in the `.Context()` method from `*sentry.Span` struct.
		// The `context.Context` instance inherits the context from `echo.Context`.
		err := expensiveThing(sentrySpan.Context())
		if err != nil {
			sentry.CaptureException(sentryfasthttp.GetContext(ctx), err)
		}

		handler(ctx)
	}
}

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if hint.Context != nil {
				if ctx, ok := hint.Context.Value(sentry.RequestContextKey).(*fasthttp.RequestCtx); ok {
					// You have access to the original Context if it panicked
					fmt.Println(string(ctx.Request.Host()))
				}
			}
			fmt.Println(event)
			return event
		},
		Debug:            true,
		AttachStacktrace: true,
	})

	sentryHandler := sentryfasthttp.New(sentryfasthttp.Options{})

	defaultHandler := func(ctx *fasthttp.RequestCtx) {
		scope := sentryfasthttp.GetScopeFromContext(ctx)
		scope.SetTag("unwantedQuery", "someQueryDataMaybe")
		sentry.CaptureMessage(sentryfasthttp.GetContext(ctx), "User provided unwanted query string, but we recovered just fine")
		ctx.SetStatusCode(fasthttp.StatusOK)
	}

	fooHandler := enhanceSentryEvent(func(ctx *fasthttp.RequestCtx) {
		panic("y tho")
	})

	fastHTTPHandler := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/foo":
			fooHandler(ctx)
		default:
			defaultHandler(ctx)
		}
	}

	fmt.Println("Listening and serving HTTP on :3000")

	if err := fasthttp.ListenAndServe(":3000", sentryHandler.Handle(fastHTTPHandler)); err != nil {
		panic(err)
	}
}
