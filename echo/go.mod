module github.com/getsentry/sentry-go/echo

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.41.0
	github.com/google/go-cmp v0.5.9
	github.com/labstack/echo/v5 v5.0.0
)

require (
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)
