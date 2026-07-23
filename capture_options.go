package sentry

import "context"

// CaptureOption configures a single event capture.
type CaptureOption interface {
	applyCaptureOption(*captureOptions)
}

type captureOptionFunc func(*captureOptions)

func (f captureOptionFunc) applyCaptureOption(options *captureOptions) { f(options) }

type captureOptions struct {
	hint           *EventHint
	modifiers      []EventModifier
	legacyScope    EventModifier
	legacyScopeSet bool
}

// WithEventHint supplies metadata to event processors and before-send hooks.
// When supplied more than once, the last hint wins.
func WithEventHint(hint *EventHint) CaptureOption {
	return captureOptionFunc(func(options *captureOptions) { options.hint = hint })
}

// WithEventModifier applies modifier to the captured event. It may be repeated.
func WithEventModifier(modifier EventModifier) CaptureOption {
	return captureOptionFunc(func(options *captureOptions) {
		if modifier != nil {
			options.modifiers = append(options.modifiers, modifier)
		}
	})
}

// withLegacyScope preserves backward-compatible Hub scope processing and should be removed with the Hub.
func withLegacyScope(scope EventModifier) CaptureOption {
	return captureOptionFunc(func(options *captureOptions) {
		options.legacyScope = scope
		options.legacyScopeSet = true
	})
}

// withDefaultOriginalException preserves a caller-supplied hint value while filling the capture default.
func withDefaultOriginalException(exception error) CaptureOption {
	return captureOptionFunc(func(options *captureOptions) {
		if options.hint == nil {
			options.hint = &EventHint{}
		}
		if options.hint.OriginalException == nil {
			hint := *options.hint
			hint.OriginalException = exception
			options.hint = &hint
		}
	})
}

// withDefaultRecoveredException preserves a caller-supplied hint value while filling the capture default.
func withDefaultRecoveredException(recovered any) CaptureOption {
	return captureOptionFunc(func(options *captureOptions) {
		if options.hint == nil {
			options.hint = &EventHint{}
		}
		if options.hint.RecoveredException == nil {
			hint := *options.hint
			hint.RecoveredException = recovered
			options.hint = &hint
		}
	})
}

func resolveCaptureOptions(ctx context.Context, client Client, options []CaptureOption) (*EventHint, EventModifier, *Scope) {
	var resolved captureOptions
	for _, option := range options {
		if option != nil {
			option.applyCaptureOption(&resolved)
		}
	}
	hint := &EventHint{}
	if resolved.hint != nil {
		*hint = *resolved.hint
	}
	// Explicit capture context overrides any supplied hint.Context. A nil ctx
	// leaves any supplied hint context in place so legacy Hub APIs and scope
	// request-context fallback continue to work.
	if ctx != nil {
		hint.Context = ctx
	}
	if resolved.legacyScopeSet {
		var modifiers []EventModifier
		if resolved.legacyScope != nil {
			modifiers = append(modifiers, resolved.legacyScope)
		}
		modifiers = append(modifiers, resolved.modifiers...)
		return hint, captureModifierChain(modifiers), nil
	}
	global := GlobalScope()
	isolation := scopeFromContext(ctx)
	owner := isolation
	if owner == nil {
		owner = global
	}
	modifiers := append([]EventModifier{snapshotScopes(client, global, isolation)}, resolved.modifiers...)
	return hint, captureModifierChain(modifiers), owner
}

type captureModifierChain []EventModifier

func (modifiers captureModifierChain) ApplyToEvent(event *Event, hint *EventHint, client Client) *Event {
	for _, modifier := range modifiers {
		if modifier != nil {
			event = modifier.ApplyToEvent(event, hint, client)
		}
		if event == nil {
			return nil
		}
	}
	return event
}
