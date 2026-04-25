module github.com/getsentry/sentry-go/rueidis

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/redis/rueidis v1.0.74
	github.com/redis/rueidis/rueidishook v1.0.74
)

require golang.org/x/sys v0.39.0 // indirect
