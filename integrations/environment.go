package sentry

import (
	"runtime"
	"sentry"
)

type EnvironmentIntegration struct{}

func (ei EnvironmentIntegration) Name() string {
	return "Environment"
}

func (ei EnvironmentIntegration) SetupOnce() {
	sentry.AddGlobalEventProcessor(ei.processor)
}

func (ei EnvironmentIntegration) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	// Run the integration only on the Client that registered it
	if sentry.CurrentHub().GetIntegration(ei.Name()) == nil {
		return event
	}

	if event.Contexts == nil {
		event.Contexts = make(map[string]interface{})
	}

	event.Contexts["device"] = map[string]interface{}{
		"arch":    runtime.GOARCH,
		"num_cpu": runtime.NumCPU(),
	}

	event.Contexts["os"] = map[string]interface{}{
		"name": runtime.GOOS,
	}

	event.Contexts["runtime"] = map[string]interface{}{
		"name":    "go",
		"version": runtime.Version(),
	}

	return event
}
