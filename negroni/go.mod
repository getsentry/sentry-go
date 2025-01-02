module github.com/getsentry/sentry-go/negroni

go 1.21

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.31.1
	github.com/google/go-cmp v0.5.9
	github.com/urfave/negroni/v3 v3.1.1
)

require (
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)
