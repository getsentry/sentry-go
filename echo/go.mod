module github.com/getsentry/sentry-go/echo

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.49.0
	github.com/google/go-cmp v0.7.0
	github.com/labstack/echo/v5 v5.2.0
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)
