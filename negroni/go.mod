module github.com/getsentry/sentry-go/negroni

go 1.17

require (
	github.com/getsentry/sentry-go v0.12.0
	github.com/urfave/negroni v1.0.0
)

require golang.org/x/sys v0.0.0-20211007075335-d3039528d8ac // indirect

replace github.com/getsentry/sentry-go => ../
