module github.com/getsentry/sentry-go/valkey

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/valkey-io/valkey-go v1.0.74
	github.com/valkey-io/valkey-go/valkeyhook v1.0.74
)

require golang.org/x/sys v0.39.0 // indirect
