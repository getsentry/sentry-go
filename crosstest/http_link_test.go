package crosstest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	sentryfasthttp "github.com/getsentry/sentry-go/fasthttp"
	sentryfiber "github.com/getsentry/sentry-go/fiber"
	sentrygin "github.com/getsentry/sentry-go/gin"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentryiris "github.com/getsentry/sentry-go/iris"
	sentrynegroni "github.com/getsentry/sentry-go/negroni"
	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	"github.com/kataras/iris/v12"
	"github.com/labstack/echo/v5"
	"github.com/urfave/negroni/v3"
	"github.com/valyala/fasthttp"
)

func sendSignals(ctx context.Context, identifier string, logger sentry.Logger, meter sentry.Meter) {
	hub := sentry.GetHubFromContext(ctx)
	hub.CaptureException(errors.New(identifier + " manual error"))
	logger.Info().WithCtx(ctx).Emit(identifier + " linked log")
	meter.WithCtx(ctx).Count(identifier+".linked.metric", 1)
	panic(identifier + " panic")
}

func requireRequestSignalsLinked(t *testing.T, events []*sentry.Event, traceID sentry.TraceID, spanID sentry.SpanID, identifier string) {
	t.Helper()
	requireLinked(t, events,
		linkedErrorEvent(traceID, spanID, identifier+" manual error"),
		linkedErrorEvent(traceID, spanID, identifier+" panic"),
		linkedLogEvent(traceID, spanID, identifier+" linked log"),
		linkedMetricEvent(traceID, spanID, identifier+".linked.metric", 1),
	)
}

func TestHTTPFamilyIntegrationsLinkManualErrorsLogsMetricsAndPanicsToOTel(t *testing.T) {
	t.Parallel()
	otelCtx, traceID, spanID := fixedOTelContext()

	t.Run("http", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "http"
			baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
			var logger sentry.Logger = sentry.NewLogger(baseCtx)
			var meter sentry.Meter = sentry.NewMeter(baseCtx)
			handler := sentryhttp.New(sentryhttp.Options{WaitForDelivery: true}).HandleFunc(func(w http.ResponseWriter, r *http.Request) {
				sendSignals(r.Context(), identifier, logger, meter)
			})

			req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
			req = req.WithContext(sentry.SetHubOnContext(otelCtx, f.Hub))
			handler.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	t.Run("gin", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "gin"
			baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
			var logger sentry.Logger = sentry.NewLogger(baseCtx)
			var meter sentry.Meter = sentry.NewMeter(baseCtx)
			gin.SetMode(gin.ReleaseMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(sentry.SetHubOnContext(otelCtx, f.Hub))
				c.Next()
			})
			router.Use(sentrygin.New(sentrygin.Options{WaitForDelivery: true}))
			router.GET("/test", func(c *gin.Context) {
				sendSignals(c.Request.Context(), identifier, logger, meter)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			router.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	t.Run("echo", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "echo"
			baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
			var logger sentry.Logger = sentry.NewLogger(baseCtx)
			var meter sentry.Meter = sentry.NewMeter(baseCtx)
			e := echo.New()
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					sentryecho.SetHubOnContext(c, f.Hub)
					c.SetRequest(c.Request().WithContext(sentry.SetHubOnContext(otelCtx, f.Hub)))
					return next(c)
				}
			})
			e.Use(sentryecho.New(sentryecho.Options{WaitForDelivery: true}))
			e.GET("/test", func(c *echo.Context) error {
				sendSignals(c.Request().Context(), identifier, logger, meter)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			e.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	t.Run("negroni", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "negroni"
			baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
			var logger sentry.Logger = sentry.NewLogger(baseCtx)
			var meter sentry.Meter = sentry.NewMeter(baseCtx)
			n := negroni.New()
			n.Use(negroni.HandlerFunc(func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
				next(w, r.WithContext(sentry.SetHubOnContext(otelCtx, f.Hub)))
			}))
			n.Use(sentrynegroni.New(sentrynegroni.Options{WaitForDelivery: true}))
			n.UseHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sendSignals(r.Context(), identifier, logger, meter)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			n.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	t.Run("iris", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "iris"
			baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
			var logger sentry.Logger = sentry.NewLogger(baseCtx)
			var meter sentry.Meter = sentry.NewMeter(baseCtx)
			app := iris.New()
			app.Use(func(ctx iris.Context) {
				ctx.ResetRequest(ctx.Request().WithContext(sentry.SetHubOnContext(otelCtx, f.Hub)))
				ctx.Next()
			})
			app.Use(sentryiris.New(sentryiris.Options{WaitForDelivery: true}))
			app.Get("/test", func(ctx iris.Context) {
				sendSignals(ctx.Request().Context(), identifier, logger, meter)
			})

			if err := app.Build(); err != nil {
				t.Fatalf("iris build: %v", err)
			}
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			app.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	// fiber uses app.Test which starts a real fasthttp listener that leaks
	// background goroutines (updateServerDate), so it cannot run inside a
	// synctest bubble.
	t.Run("fiber", func(t *testing.T) {
		t.Parallel()
		f := sentrytest.NewFixture(t, otelOpts()...)
		const identifier = "fiber"
		baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
		var logger sentry.Logger = sentry.NewLogger(baseCtx)
		var meter sentry.Meter = sentry.NewMeter(baseCtx)
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.SetUserContext(sentry.SetHubOnContext(otelCtx, f.Hub))
			sentryfiber.SetHubOnContext(c, f.Hub)
			return c.Next()
		})
		app.Use(sentryfiber.New(sentryfiber.Options{WaitForDelivery: true}))
		app.Get("/test", func(c *fiber.Ctx) error {
			sendSignals(c.UserContext(), identifier, logger, meter)
			return nil
		})

		req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
		if _, err := app.Test(req); err != nil {
			t.Fatalf("fiber request: %v", err)
		}

		f.Flush()
		requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
	})
}

func TestFastHTTPOTelValidationGap(t *testing.T) {
	_ = sentryfasthttp.New
	_ = fasthttp.RequestCtx{}
	t.Skip("fasthttp does not preserve a standard request context that the OTel integration can resolve automatically today")
}
