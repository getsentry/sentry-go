module github.com/getsentry/sentry-go/otel

go 1.19

require (
	github.com/getsentry/sentry-go v0.16.0
	go.opentelemetry.io/otel v1.11.2
	go.opentelemetry.io/otel/sdk v1.11.2
	go.opentelemetry.io/otel/trace v1.11.2
)

// TODO(anton): Should we remove this before releasing?
replace github.com/getsentry/sentry-go => ../

require (
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	golang.org/x/sys v0.3.0 // indirect
	golang.org/x/text v0.5.0 // indirect
)
