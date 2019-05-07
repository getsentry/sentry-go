package sentry

import (
	"io/ioutil"
	"net/http"
	"sentry"
)

type RequestIntegration struct{}

func (ri RequestIntegration) Name() string {
	return "Request"
}

func (ri RequestIntegration) SetupOnce() {
	sentry.AddGlobalEventProcessor(ri.processor)
}

func (ri RequestIntegration) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	// Run the integration only on the Client that registered it
	if sentry.CurrentHub().GetIntegration(ri.Name()) == nil {
		return event
	}

	if hint == nil {
		return event
	}

	if hint.Request != nil {
		return ri.fillEvent(event, hint.Request)
	}

	if request, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
		return ri.fillEvent(event, request)
	}

	return event
}

func (ri RequestIntegration) fillEvent(event *sentry.Event, request *http.Request) *sentry.Event {
	event.Request.Method = request.Method
	event.Request.Cookies = request.Cookies()
	event.Request.Headers = request.Header
	event.Request.URL = request.URL.String()
	event.Request.QueryString = request.URL.RawQuery
	if body, err := ioutil.ReadAll(request.Body); err == nil {
		event.Request.Data = string(body)
	}
	return event
}
