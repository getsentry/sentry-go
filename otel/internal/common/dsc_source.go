package common

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/trace"
)

type DSCSource interface {
	GetDSC(trace.TraceID, trace.SpanID) (sentry.DynamicSamplingContext, bool)
}

type noopDSCSource struct{}

func (n *noopDSCSource) GetDSC(trace trace.TraceID, span trace.SpanID) (sentry.DynamicSamplingContext, bool) {
	return sentry.DynamicSamplingContext{}, false
}
