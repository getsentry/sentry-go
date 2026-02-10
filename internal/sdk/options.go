package sdk

import "github.com/getsentry/sentry-go/internal/report"

// Option configures SDK components.
type Option func(*Options)

// Options holds optional dependencies shared across SDK components.
type Options struct {
	Reporter *report.Aggregator
}

// WithReporter sets the client report aggregator for tracking discarded events.
func WithReporter(r *report.Aggregator) Option {
	return func(o *Options) {
		o.Reporter = r
	}
}

// Apply resolves the given options into an Options struct.
func Apply(opts []Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}
