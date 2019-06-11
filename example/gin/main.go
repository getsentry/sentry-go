package main

import (
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
		Debug:            true,
		AttachStacktrace: true,
	})

	r := gin.Default()
	r.Use(sentrygin.New(sentrygin.Options{
		Repanic:         true,
		WaitForDelivery: true,
	}))
	r.GET("/", func(c *gin.Context) {
		panic("y tho")
	})
	_ = r.Run(":3000")
}
