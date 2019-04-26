package sentry

import (
	"runtime"
	"sentry"
)

type EnvContext struct{}

func (envContext *EnvContext) Name() string {
	return "EnvContext"
}

func (envContext *EnvContext) SetupOnce() {
	sentry.AddGlobalEventProcessor(func(event *sentry.Event) *sentry.Event {
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
	})
}
