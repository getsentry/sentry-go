package sentryfiber

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/utils"
)

const valuesKey = "sentry"

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to false,
	// as fasthttp doesn't include it's own Recovery handler.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because fasthttp doesn't include it's own Recovery handler, it will restart the application,
	// and event won't be delivered otherwise.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

func New(options Options) fiber.Handler {
	handler := handler{
		repanic:         false,
		timeout:         time.Second * 2,
		waitForDelivery: false,
	}

	if options.Repanic {
		handler.repanic = true
	}

	if options.Timeout != 0 {
		handler.timeout = options.Timeout
	}

	if options.WaitForDelivery {
		handler.waitForDelivery = true
	}

	return handler.handle
}

func (h *handler) handle(ctx *fiber.Ctx) error {
	hub := sentry.CurrentHub().Clone()
	scope := hub.Scope()
	scope.SetRequest(convert(ctx))
	scope.SetRequestBody(ctx.Request().Body())
	ctx.Locals(valuesKey, hub)
	defer h.recoverWithSentry(hub, ctx)
	return ctx.Next()
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, ctx *fiber.Ctx) {
	if err := recover(); err != nil {
		eventID := hub.RecoverWithContext(
			context.WithValue(context.Background(), sentry.RequestContextKey, ctx),
			err,
		)
		if eventID != nil && h.waitForDelivery {
			hub.Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}

func GetHubFromContext(ctx *fiber.Ctx) *sentry.Hub {
	hub := ctx.Locals(valuesKey)
	if hub, ok := hub.(*sentry.Hub); ok {
		return hub
	}
	return nil
}

func convert(ctx *fiber.Ctx) *http.Request {
	defer func() {
		if err := recover(); err != nil {
			sentry.Logger.Printf("%v", err)
		}
	}()

	r := new(http.Request)

	r.Method = utils.ImmutableString(ctx.Method())
	uri := ctx.Request().URI()
	r.URL, _ = url.Parse(fmt.Sprintf("%s://%s%s", uri.Scheme(), uri.Host(), uri.Path()))

	// Headers
	r.Header = make(http.Header)
	ctx.Request().Header.VisitAll(func(key, value []byte) {
		r.Header.Add(string(key), string(value))
	})
	r.Host = utils.ImmutableString(ctx.Hostname())

	// Cookies
	ctx.Request().Header.VisitAllCookie(func(key, value []byte) {
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)})
	})

	// Env
	r.RemoteAddr = ctx.Context().RemoteAddr().String()

	// QueryString
	r.URL.RawQuery = string(ctx.Request().URI().QueryString())

	// Body
	r.Body = ioutil.NopCloser(bytes.NewReader(ctx.Request().Body()))

	return r
}
