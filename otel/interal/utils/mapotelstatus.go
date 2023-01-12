package utils

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
)

func MapOtelStatus(s trace.ReadOnlySpan) sentry.SpanStatus {
	statusCode := s.Status().Code

	if statusCode == codes.Unset {
		return sentry.SpanStatusUnknown
	}

	if statusCode == codes.Error {
		return sentry.SpanStatusInternalError
	}

	if statusCode == codes.Ok {
		return sentry.SpanStatusOK
	}

	return sentry.SpanStatusUnknown

	// TODO(michi) Add http and grpc codes
}
