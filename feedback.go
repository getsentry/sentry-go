package sentry

// Feedback contains user-provided feedback to send to Sentry.
//
// Message is the only required field. AssociatedEventID may be set to link the
// feedback to a previously captured event.
type Feedback struct {
	Message           string
	Name              string
	Email             string
	URL               string
	Source            string
	AssociatedEventID EventID
}

func (feedback *Feedback) context() Context {
	context := Context{
		"message": feedback.Message,
	}

	if feedback.Name != "" {
		context["name"] = feedback.Name
	}
	if feedback.Email != "" {
		context["contact_email"] = feedback.Email
	}
	if feedback.URL != "" {
		context["url"] = feedback.URL
	}
	if feedback.Source != "" {
		context["source"] = feedback.Source
	}
	if feedback.AssociatedEventID != "" {
		context["associated_event_id"] = feedback.AssociatedEventID
	}

	return context
}
