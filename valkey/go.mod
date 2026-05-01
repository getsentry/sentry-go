module github.com/getsentry/sentry-go/valkey

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.46.0
	github.com/stretchr/testify v1.11.1
	github.com/valkey-io/valkey-go v1.0.74
	github.com/valkey-io/valkey-go/mock v1.0.74
	github.com/valkey-io/valkey-go/valkeyhook v1.0.74
	go.uber.org/mock v0.6.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
