module github.com/getsentry/sentry-go/negroni

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.49.0
	github.com/google/go-cmp v0.7.0
	github.com/urfave/negroni/v3 v3.1.1
)

require (
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
