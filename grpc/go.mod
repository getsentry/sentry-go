module github.com/getsentry/sentry-go/grpc

go 1.26.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v1.0.0-alpha.1
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.12.1
	google.golang.org/grpc v1.83.2
)

require (
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.45.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
