module github.com/getsentry/sentry-go/logrus

go 1.21

replace github.com/getsentry/sentry-go => ../

require (
	github.com/getsentry/sentry-go v0.31.1
	github.com/google/go-cmp v0.6.0
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
)

require (
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)
