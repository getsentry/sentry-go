package sentry

import "context"

// CaptureOption configures a single event capture.
type CaptureOption func(*captureOptions)

// WithEventHint supplies metadata to event processors and before-send hooks.
func WithEventHint(hint *EventHint) CaptureOption {
	return func(options *captureOptions) { options.hint = hint }
}

// WithLevel sets an explicit level for the capture.
func WithLevel(level Level) CaptureOption {
	return func(options *captureOptions) { options.level = level }
}

func resolveCaptureOptions(ctx context.Context, options []CaptureOption) captureOptions {
	var resolved captureOptions
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}

	hint := &EventHint{}
	if resolved.hint != nil {
		*hint = *resolved.hint
	}
	if ctx != nil {
		hint.Context = ctx
	}
	resolved.hint = hint
	return resolved
}
