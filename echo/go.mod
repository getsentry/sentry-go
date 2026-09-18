module github.com/getsentry/sentry-go/echo

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.49.0
	github.com/google/go-cmp v0.7.0
	github.com/labstack/echo/v5 v5.2.0
)

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
