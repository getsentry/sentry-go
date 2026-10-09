module github.com/getsentry/sentry-go/echo

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.50.0
	github.com/google/go-cmp v0.7.0
	github.com/labstack/echo/v5 v5.2.0
)

require (
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
