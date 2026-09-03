package sentry

import "context"

// CaptureOption configures a single event capture.
type CaptureOption func(*captureOptions)

type captureOptions struct {
	hint         *EventHint
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
	if ctx != nil {
		hint.Context = ctx
	}
	resolved.hint = hint
	return resolved
}
