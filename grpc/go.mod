module github.com/getsentry/sentry-go/grpc

go 1.22

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.30.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241015192408-796eee8c2d53
	google.golang.org/grpc v1.69.2
)

require (
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
)
