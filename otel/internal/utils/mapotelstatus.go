package utils

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
)

// // OpenTelemetry span status can be Unset, Ok, Error. HTTP and Grpc codes contained in tags can make it more detailed.

// canonicalCodesHTTPMap maps some HTTP codes to Sentry's span statuses. See possible mapping in https://develop.sentry.dev/sdk/event-payloads/span/
var canonicalCodesHTTPMap = map[string]sentry.SpanStatus{
	"400": sentry.SpanStatusFailedPrecondition, // SpanStatusInvalidArgument, SpanStatusOutOfRange
	"401": sentry.SpanStatusUnauthenticated,
	"403": sentry.SpanStatusPermissionDenied,
	"404": sentry.SpanStatusNotFound,
	"409": sentry.SpanStatusAborted, // SpanStatusAlreadyExists
	"429": sentry.SpanStatusResourceExhausted,
	"499": sentry.SpanStatusCanceled,
	"500": sentry.SpanStatusInternalError, // SpanStatusDataLoss, SpanStatusUnknown
	"501": sentry.SpanStatusUnimplemented,
	"503": sentry.SpanStatusUnavailable,
	"504": sentry.SpanStatusDeadlineExceeded,
}

// canonicalCodesGrpcMap maps some GRPC codes to Sentry's span statuses. See description in grpc documentation.
var canonicalCodesGrpcMap = map[string]sentry.SpanStatus{
	"1":  sentry.SpanStatusCanceled,
	"2":  sentry.SpanStatusUnknown,
	"3":  sentry.SpanStatusInvalidArgument,
	"4":  sentry.SpanStatusDeadlineExceeded,
	"5":  sentry.SpanStatusNotFound,
	"6":  sentry.SpanStatusAlreadyExists,
	"7":  sentry.SpanStatusPermissionDenied,
	"8":  sentry.SpanStatusResourceExhausted,
	"9":  sentry.SpanStatusFailedPrecondition,
	"10": sentry.SpanStatusAborted,
	"11": sentry.SpanStatusOutOfRange,
	"12": sentry.SpanStatusUnimplemented,
	"13": sentry.SpanStatusInternalError,
	"14": sentry.SpanStatusUnavailable,
	"15": sentry.SpanStatusDataLoss,
	"16": sentry.SpanStatusUnauthenticated,
}

func MapOtelStatus(s trace.ReadOnlySpan) sentry.SpanStatus {
	statusCode := s.Status().Code

	for _, attribute := range s.Attributes() {
		if attribute.Key == semconv.HTTPStatusCodeKey {
			if status, ok := canonicalCodesHTTPMap[attribute.Value.Emit()]; ok {
				return status
			}
		}

		if attribute.Key == semconv.RPCGRPCStatusCodeKey {
			if status, ok := canonicalCodesGrpcMap[attribute.Value.Emit()]; ok {
				return status
			}
		}
	}

	if statusCode == codes.Unset || statusCode == codes.Ok {
		return sentry.SpanStatusOK
	}

	if statusCode == codes.Error {
		return sentry.SpanStatusInternalError
	}

	return sentry.SpanStatusUnknown
}
