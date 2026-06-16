module github.com/getsentry/sentry-go/fasthttp

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.46.2
	github.com/google/go-cmp v0.7.0
	github.com/valyala/fasthttp v1.71.0
)

require (
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
