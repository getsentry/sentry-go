module github.com/getsentry/sentry-go/grpc

go 1.24.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.43.0
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.10.0
	google.golang.org/grpc v1.79.3
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
