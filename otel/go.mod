module github.com/getsentry/sentry-go/otel

go 1.18

require (
	github.com/getsentry/sentry-go v0.18.0
	github.com/google/go-cmp v0.5.9
	go.opentelemetry.io/otel v1.11.0
	go.opentelemetry.io/otel/sdk v1.11.0
	go.opentelemetry.io/otel/trace v1.11.0
)

replace github.com/getsentry/sentry-go => ../

require (
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	golang.org/x/sys v0.4.0 // indirect
	golang.org/x/text v0.6.0 // indirect
)
