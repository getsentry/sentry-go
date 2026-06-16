module github.com/getsentry/sentry-go/echo

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.47.0
	github.com/google/go-cmp v0.7.0
	github.com/labstack/echo/v5 v5.0.3
)

require (
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
