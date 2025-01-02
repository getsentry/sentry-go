module github.com/getsentry/sentry-go/echo

go 1.21

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.31.1
	github.com/google/go-cmp v0.5.9
	github.com/labstack/echo/v4 v4.10.0
)

require (
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)
