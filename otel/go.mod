module github.com/getsentry/sentry-go/otel

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v1.0.0-alpha.1
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otel/trace v1.47.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)
