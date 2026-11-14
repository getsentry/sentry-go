module github.com/getsentry/sentry-go/sql

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/DataDog/go-sqllexer v0.2.1
	github.com/getsentry/sentry-go v1.0.0-alpha.1
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect
