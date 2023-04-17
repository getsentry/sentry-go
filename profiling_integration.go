package sentry

import (
	"time"
)

type profilingIntegration struct{}

func (pi *profilingIntegration) Name() string {
	return "Profiling"
}

func (pi *profilingIntegration) SetupOnce(client *Client) {
	client.profilerFactory = func() transactionProfiler {
		return &_transactionProfiler{
			stopFunc: startProfiling(),
		}
	}
}

type transactionProfiler interface {
	Finish(span *Span, event *Event) *profileInfo
}

type _transactionProfiler struct {
	stopFunc func() *profileTrace
}

// Finish implements transactionProfiler
func (tp *_transactionProfiler) Finish(span *Span, event *Event) *profileInfo {
	trace := tp.stopFunc()
	info := &profileInfo{
		Version:     "1",
		Environment: event.Environment,
		EventID:     uuid(),
		Platform:    "go",
		Release:     event.Release,
		Timestamp:   span.StartTime, // TODO: use profiler StartTime? Does it make a difference?
		Trace:       trace,
		Transaction: profileTransaction{
			ActiveThreadID: 0,
			DurationNS:     uint64(time.Since(span.StartTime).Nanoseconds()),
			ID:             "", // Event ID not available here yet
			Name:           span.Name,
			TraceID:        span.TraceID.String(),
		},
	}
	if runtimeContext, ok := event.Contexts["runtime"]; ok {
		if value, ok := runtimeContext["name"]; !ok {
			info.Runtime.Name = value.(string)
		}
		if value, ok := runtimeContext["version"]; !ok {
			info.Runtime.Version = value.(string)
		}
	}
	if osContext, ok := event.Contexts["os"]; ok {
		if value, ok := osContext["name"]; !ok {
			info.OS.Name = value.(string)
		}
	}
	if deviceContext, ok := event.Contexts["device"]; ok {
		if value, ok := deviceContext["arch"]; !ok {
			info.Device.Architecture = value.(string)
		}
	}
	return info
}
