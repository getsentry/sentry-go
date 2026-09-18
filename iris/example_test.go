package sentryiris_test

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/kataras/iris/v12"
)

func Example() {
	app := iris.New()
	app.Use(sentryiris.New(sentryiris.Options{}))
	app.Get("/", func(ctx iris.Context) {
		expensiveThing := func(ctx context.Context) {
			span := sentry.StartSpan(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
		}

		// Acquire the transaction from the request context. It may be nil if
		// you did not set up the sentryiris middleware.
		spanContext := ctx.Request().Context()
		if sentrySpan := sentry.SpanFromContext(spanContext); sentrySpan != nil {
			spanContext = sentrySpan.Context()
		}
		expensiveThing(spanContext)

		ctx.StatusCode(http.StatusOK)
	})
}
