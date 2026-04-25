module github.com/getsentry/sentry-go/rueidis

go 1.25.0

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.46.0
	github.com/redis/rueidis v1.0.74
	github.com/redis/rueidis/rueidishook v1.0.74
)

require (
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
