package sentry

import "context"

var ExtractStacktrace = extractStacktrace

// ApplyToEvent applies scope to event and runs the scope's event processors.
func (scope *Scope) ApplyToEvent(event *Event, hint *EventHint, client *Client) *Event {
	client = normalizeClient(client)
	var ctx context.Context
	if hint != nil {
		ctx = hint.Context
	}
	processors := scope.applyToEvent(ctx, event, client, client.options.MaxBreadcrumbs)
	return client.runEventProcessors(event, hint, processors)
}
