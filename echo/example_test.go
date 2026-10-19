package sentryecho_test

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v5"
)

func Example() {
	router := echo.New()
	router.Use(sentryecho.New(sentryecho.Options{}))
	router.GET("/", func(c *echo.Context) error {
		expensiveThing := func(ctx context.Context) error {
			span := sentry.StartSpan(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
			return nil
		}

		err := expensiveThing(c.Request().Context())
		if err != nil {
			return err
		}

		return c.NoContent(http.StatusOK)
	})
}
