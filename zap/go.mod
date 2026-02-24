module github.com/getsentry/sentry-go/zap

go 1.24.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.0.0-00010101000000-000000000000
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.27.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
