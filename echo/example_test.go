package sentryecho_test

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v5"
)

func ExampleGetSpanFromContext() {
	router := echo.New()
	router.Use(sentryecho.New(sentryecho.Options{}))
	router.GET("/", func(c *echo.Context) error {
		expensiveThing := func(ctx context.Context) error {
			span := sentry.StartTransaction(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
			return nil
		}

		// Acquire the transaction from the request context. It may be nil if
		// you did not set up the sentryecho middleware.
		spanContext := c.Request().Context()
		if sentrySpan := sentryecho.GetSpanFromContext(c); sentrySpan != nil {
			spanContext = sentrySpan.Context()
		}
		err := expensiveThing(spanContext)
		if err != nil {
			return err
		}

		return c.NoContent(http.StatusOK)
	})
}
