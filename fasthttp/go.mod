module github.com/getsentry/sentry-go/fasthttp

go 1.17

require (
	github.com/getsentry/sentry-go v0.12.0
	github.com/google/go-cmp v0.5.5
	github.com/valyala/fasthttp v1.6.0
)

require (
	github.com/klauspost/compress v1.9.7 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	golang.org/x/sys v0.0.0-20211007075335-d3039528d8ac // indirect
	golang.org/x/xerrors v0.0.0-20191204190536-9bdfabe68543 // indirect
)

replace github.com/getsentry/sentry-go => ../
