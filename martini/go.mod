module github.com/getsentry/sentry-go/martini

go 1.17

require (
	github.com/getsentry/sentry-go v0.12.0
	github.com/go-martini/martini v0.0.0-20170121215854-22fa46961aab
)

require (
	github.com/codegangsta/inject v0.0.0-20150114235600-33e0aa1cb7c0 // indirect
	golang.org/x/sys v0.0.0-20211007075335-d3039528d8ac // indirect
)

replace github.com/getsentry/sentry-go => ../
