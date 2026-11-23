module github.com/getsentry/sentry-go/zerolog

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/buger/jsonparser v1.1.2
	github.com/getsentry/sentry-go v1.0.0-alpha.1
	github.com/rs/zerolog v1.33.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
