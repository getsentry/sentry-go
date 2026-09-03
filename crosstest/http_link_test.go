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
	sentryfiberv3 "github.com/getsentry/sentry-go/fiberv3"
	sentrygin "github.com/getsentry/sentry-go/gin"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentryiris "github.com/getsentry/sentry-go/iris"
	sentrynegroni "github.com/getsentry/sentry-go/negroni"
	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	fiberv3 "github.com/gofiber/fiber/v3"
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

func sendContextSignals(ctx context.Context, identifier string, logger sentry.Logger, meter sentry.Meter) {
	sentry.CaptureException(ctx, errors.New(identifier+" manual error"))
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
			baseCtx := f.NewContext(context.Background())
			logger := sentry.NewLogger(baseCtx)
			meter := sentry.NewMeter(baseCtx)
			handler := sentryhttp.New(sentryhttp.Options{WaitForDelivery: true}).HandleFunc(func(_ http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				sentry.CaptureException(ctx, errors.New(identifier+" manual error"))
				logger.Info().WithCtx(ctx).Emit(identifier + " linked log")
				meter.WithCtx(ctx).Count(identifier+".linked.metric", 1)
				panic(identifier + " panic")
			})

			req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
			req = req.WithContext(f.NewContext(otelCtx))
			handler.ServeHTTP(httptest.NewRecorder(), req)

			f.Flush()
			requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
		}, otelOpts()...)
	})

	t.Run("gin", func(t *testing.T) {
		t.Parallel()
		sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
			const identifier = "gin"
			baseCtx := f.NewContext(context.Background())
			logger := sentry.NewLogger(baseCtx)
			meter := sentry.NewMeter(baseCtx)
			gin.SetMode(gin.ReleaseMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(f.NewContext(otelCtx))
				c.Next()
			})
			router.Use(sentrygin.New(sentrygin.Options{WaitForDelivery: true}))
			router.GET("/test", func(c *gin.Context) {
				sendContextSignals(c.Request.Context(), identifier, logger, meter)
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
			baseCtx := f.NewContext(context.Background())
			logger := sentry.NewLogger(baseCtx)
			meter := sentry.NewMeter(baseCtx)
			e := echo.New()
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.SetRequest(c.Request().WithContext(f.NewContext(otelCtx)))
					return next(c)
				}
			})
			e.Use(sentryecho.New(sentryecho.Options{WaitForDelivery: true}))
			e.GET("/test", func(c *echo.Context) error {
				sendContextSignals(c.Request().Context(), identifier, logger, meter)
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
			baseCtx := f.NewContext(context.Background())
			logger := sentry.NewLogger(baseCtx)
			meter := sentry.NewMeter(baseCtx)
			n := negroni.New()
			n.Use(negroni.HandlerFunc(func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
				next(w, r.WithContext(f.NewContext(otelCtx)))
			}))
			n.Use(sentrynegroni.New(sentrynegroni.Options{WaitForDelivery: true}))
			n.UseHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				sendContextSignals(r.Context(), identifier, logger, meter)
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
			baseCtx := f.NewContext(context.Background())
			logger := sentry.NewLogger(baseCtx)
			meter := sentry.NewMeter(baseCtx)
			app := iris.New()
			app.Use(func(ctx iris.Context) {
				ctx.ResetRequest(ctx.Request().WithContext(f.NewContext(otelCtx)))
				ctx.Next()
			})
			app.Use(sentryiris.New(sentryiris.Options{WaitForDelivery: true}))
			app.Get("/test", func(ctx iris.Context) {
				sendContextSignals(ctx.Request().Context(), identifier, logger, meter)
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
		logger := sentry.NewLogger(baseCtx)
		meter := sentry.NewMeter(baseCtx)
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

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("fiber request: %v", err)
		}
		defer resp.Body.Close()

		f.Flush()
		requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
	})

	t.Run("fiberv3", func(t *testing.T) {
		t.Parallel()
		f := sentrytest.NewFixture(t, otelOpts()...)
		const identifier = "fiberv3"
		baseCtx := sentry.SetHubOnContext(context.Background(), f.Hub)
		logger := sentry.NewLogger(baseCtx)
		meter := sentry.NewMeter(baseCtx)
		app := fiberv3.New()
		app.Use(func(c fiberv3.Ctx) error {
			c.SetContext(sentry.SetHubOnContext(otelCtx, f.Hub))
			sentryfiberv3.SetHubOnContext(c, f.Hub)
			return c.Next()
		})
		app.Use(sentryfiberv3.New(sentryfiberv3.Options{WaitForDelivery: true}))
		app.Get("/test", func(c fiberv3.Ctx) error {
			sendSignals(c.Context(), identifier, logger, meter)
			return nil
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("fiberv3 request: %v", err)
		}
		defer resp.Body.Close()

		f.Flush()
		requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
	})
}

func TestFastHTTPOTelValidationGap(t *testing.T) {
	_ = sentryfasthttp.New
	_ = fasthttp.RequestCtx{}
	t.Skip("fasthttp does not preserve a standard request context that the OTel integration can resolve automatically today")
}
