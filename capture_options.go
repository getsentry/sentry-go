package sentry

import "context"

// CaptureOption configures a single event capture.
type CaptureOption func(*captureOptions)

type captureOptions struct {
	hint         *EventHint
	mechanism    *Mechanism
	level        Level
	defaultLevel Level
}

// WithEventHint supplies metadata to event processors and before-send hooks.
func WithEventHint(hint *EventHint) CaptureOption {
	return func(options *captureOptions) {
		options.hint = hint
	}
}

// WithLevel sets the event level for a single capture.
func WithLevel(level Level) CaptureOption {
	return func(options *captureOptions) {
		options.level = level
	}
}

// WithMechanism sets the exception mechanism for a single capture, describing
// how the exception was captured and whether it was handled. Integrations use
// it to report their own mechanism type in place of the default "generic":
//
//	sentry.Recover(ctx, recovered, sentry.WithMechanism(&sentry.Mechanism{
//		Type:    "auto.http.go.gin.middleware",
//		Handled: new(false),
//	}))
//
// The mechanism is applied to the outermost exception of the event, which is
// the error passed to CaptureException or Recover. Exceptions further down the
// error chain keep their "chained" mechanism, and the fields linking the chain
// together (ExceptionID, ParentID, Source and IsExceptionGroup) are kept from
// the mechanism the SDK generated. An empty Type keeps the generated type, or
// falls back to "generic". The option has no effect on events without
// exceptions, such as messages or panics with a non-error value.
func WithMechanism(mechanism *Mechanism) CaptureOption {
	return func(options *captureOptions) {
		options.mechanism = mechanism
	}
}

func resolveCaptureOptions(ctx context.Context, options ...CaptureOption) captureOptions {
	if len(options) == 0 {
		return captureOptions{hint: &EventHint{Context: ctx}}
	}
	return resolveCaptureOptionsWithOptions(ctx, options)
}

func resolveCaptureOptionsWithOptions(ctx context.Context, options []CaptureOption) captureOptions {
	var resolved captureOptions
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}

	hint := new(EventHint)
	if resolved.hint != nil {
		*hint = *resolved.hint
	}
	if hint.Context == nil && ctx != nil {
		hint.Context = ctx
	}
	resolved.hint = hint
	return resolved
}

// BreadcrumbOption configures a single breadcrumb operation.
type BreadcrumbOption func(*breadcrumbOptions)

type breadcrumbOptions struct {
	hint *BreadcrumbHint
}

// WithBreadcrumbHint supplies metadata to the before-breadcrumb callback.
func WithBreadcrumbHint(hint *BreadcrumbHint) BreadcrumbOption {
	return func(options *breadcrumbOptions) {
		options.hint = hint
	}
}
