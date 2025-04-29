# Sentry SDK for GoFrame

[![Go Reference](https://pkg.go.dev/badge/github.com/getsentry/sentry-go/goframe.svg)](https://pkg.go.dev/github.com/getsentry/sentry-go/goframe)

This package provides middleware for the [GoFrame](https://github.com/gogf/gf) web framework that integrates with [Sentry](https://sentry.io/) for error monitoring and performance tracking.

## Installation

```bash
go get github.com/getsentry/sentry-go/goframe
```

## Getting Started

```go
package main

import (
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/goframe"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

func main() {
	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-sentry-dsn",
		// Set traces sample rate to capture transactions
		TracesSampleRate: 1.0,
	})
	if err != nil {
		// Handle initialization error
		panic(err)
	}
	// Flush buffered events before the program terminates
	defer sentry.Flush(2 * time.Second)

	// Create GoFrame server
	s := gf.Server()

	// Use Sentry middleware
	s.Use(sentrygoframe.New(sentrygoframe.Options{
		Repanic: true,
		WaitForDelivery: false,
		Timeout: 2 * time.Second,
	}))

	// Your routes
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.GET("/", func(r *ghttp.Request) {
			r.Response.Write("Hello, World!")
		})

		group.GET("/error", func(r *ghttp.Request) {
			// This will be captured by Sentry
			panic("An example error")
		})
	})

	// Start server
	s.Run()
}
```
