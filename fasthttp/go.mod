module github.com/getsentry/sentry-go/fasthttp

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v1.0.0-alpha.1
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.12.1
	github.com/valyala/fasthttp v1.71.0
)

require (
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/klauspost/compress v1.20.2 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)
