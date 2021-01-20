package sentrygrpc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type options struct {
	repanic  bool
	reportOn ReportOn
}

func buildOptions(ff ...Option) options {
	opts := options{
		reportOn: ReportAlways,
	}

	for _, f := range ff {
		f(&opts)
	}

	return opts
}

// Option configures reporting behavior.
type Option func(*options)

// WithRepanic configures whether to panic again after recovering from
// a panic. Use this option if you have other panic handlers.
func WithRepanic(b bool) Option {
	return func(o *options) {
		o.repanic = b
	}
}

// WithReportOn configures whether to report on errors.
func WithReportOn(r ReportOn) Option {
	return func(o *options) {
		o.reportOn = r
	}
}

// ReportOn decides error should be reported to sentry.
type ReportOn func(error) bool

// ReportAlways returns true if err is non-nil.
func ReportAlways(err error) bool {
	return err != nil
}

// ReportOnCodes returns true if error code matches on of the given codes.
func ReportOnCodes(cc ...codes.Code) ReportOn {
	return func(err error) bool {
		c := status.Code(err)
		for i := range cc {
			if c == cc[i] {
				return true
			}
		}

		return false
	}
}
