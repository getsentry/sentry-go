package sentryiris_test

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/kataras/iris/v12"
)

func ExampleGetSpanFromContext() {
	app := iris.New()
	app.Use(sentryiris.New(sentryiris.Options{}))
	app.Get("/", func(ctx iris.Context) {
		expensiveThing := func(ctx context.Context) error {
			span := sentry.StartSpan(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
			return nil
		}

		// Acquire transaction on current hub that's created by the SDK.
		// Be careful, it might be a nil value if you didn't set up sentryiris middleware.
		sentrySpan := sentryiris.GetSpanFromContext(ctx)
		// Pass in the `.Context()` method from `*sentry.Span` struct.
		// The `context.Context` instance inherits the context from `iris.Context`.
		err := expensiveThing(sentrySpan.Context())
		if err != nil {
			// Handle your error
			ctx.StatusCode(http.StatusInternalServerError)
			return
		}

		ctx.StatusCode(http.StatusOK)
	})
}
