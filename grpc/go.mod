module github.com/getsentry/sentry-go/grpc

go 1.21

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.30.0
	github.com/stretchr/testify v1.10.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241223144023-3abc09e42ca8
	google.golang.org/grpc v1.67.3
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/protobuf v1.36.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
