module github.com/getsentry/sentry-go/otel

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.46.2
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/otel/trace v1.43.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
