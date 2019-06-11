package main

import (
	"github.com/getsentry/sentry-go"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/kataras/iris"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
		Debug:            true,
		AttachStacktrace: true,
	})

	app := iris.Default()
	app.Use(sentryiris.New(sentryiris.Options{
		Repanic:         true,
		WaitForDelivery: true,
	}))
	app.Get("/", func(ctx iris.Context) {
		panic("y tho")
	})
	_ = app.Run(iris.Addr(":3000"))
}
