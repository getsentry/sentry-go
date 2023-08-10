package utils_test

import (
	"fmt"
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"strconv"
	"testing"
)

type mockReadOnlySpan struct {
	trace.ReadOnlySpan
	status     trace.Status
	attributes []attribute.KeyValue
}

var _ trace.ReadOnlySpan = new(mockReadOnlySpan)

func (m *mockReadOnlySpan) Attributes() []attribute.KeyValue {
	return m.attributes
}

func (m *mockReadOnlySpan) Status() trace.Status {
	return m.status
}

func TestMapOtelStatus(t *testing.T) {
	t.Run("Given no meaningful attributes to derive the status", func(t *testing.T) {
		tests := []struct {
			name string
			span trace.ReadOnlySpan
			want sentry.SpanStatus
		}{
			{
				name: "Should return SpanStatusOk if given a Ok status",
				span: &mockReadOnlySpan{
					status: trace.Status{
						Code: codes.Ok,
					},
				},
				want: sentry.SpanStatusOK,
			},
			{
				name: "Should return SpanStatusOk if given a Unset status",
				span: &mockReadOnlySpan{
					status: trace.Status{
						Code: codes.Unset,
					},
				},
				want: sentry.SpanStatusOK,
			},
			{
				name: "Should return SpanStatusError if given a Error status",
				span: &mockReadOnlySpan{
					status: trace.Status{
						Code: codes.Error,
					},
				},
				want: sentry.SpanStatusInternalError,
			},
			{
				name: "Should return SpanStatusUnknown if given an unknown status",
				span: &mockReadOnlySpan{
					status: trace.Status{
						Code: 1337,
					},
				},
				want: sentry.SpanStatusUnknown,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := utils.MapOtelStatus(tt.span); got != tt.want {
					t.Errorf("MapOtelStatus() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	asInt := func(key attribute.Key, i int) (attribute.KeyValue, string) {
		return key.Int(i), "as an int attribute"
	}

	asString := func(key attribute.Key, i int) (attribute.KeyValue, string) {
		return key.String(strconv.Itoa(i)), "as a string attribute"
	}

	t.Run("Given a HTTP Status code", func(t *testing.T) {
		tts := []struct {
			code    int
			factory func(key attribute.Key, i int) (attribute.KeyValue, string)
			want    sentry.SpanStatus
		}{
			{400, asInt, sentry.SpanStatusFailedPrecondition},
			{401, asInt, sentry.SpanStatusUnauthenticated},
			{403, asInt, sentry.SpanStatusPermissionDenied},
			{404, asInt, sentry.SpanStatusNotFound},
			{409, asInt, sentry.SpanStatusAborted},
			{429, asInt, sentry.SpanStatusResourceExhausted},
			{499, asInt, sentry.SpanStatusCanceled},
			{500, asInt, sentry.SpanStatusInternalError},
			{501, asInt, sentry.SpanStatusUnimplemented},
			{503, asInt, sentry.SpanStatusUnavailable},
			{504, asInt, sentry.SpanStatusDeadlineExceeded},
			{400, asString, sentry.SpanStatusFailedPrecondition},
			{401, asString, sentry.SpanStatusUnauthenticated},
			{403, asString, sentry.SpanStatusPermissionDenied},
			{404, asString, sentry.SpanStatusNotFound},
			{409, asString, sentry.SpanStatusAborted},
			{429, asString, sentry.SpanStatusResourceExhausted},
			{499, asString, sentry.SpanStatusCanceled},
			{500, asString, sentry.SpanStatusInternalError},
			{501, asString, sentry.SpanStatusUnimplemented},
			{503, asString, sentry.SpanStatusUnavailable},
			{504, asString, sentry.SpanStatusDeadlineExceeded},
		}

		for _, tt := range tts {
			attr, how := tt.factory(semconv.HTTPStatusCodeKey, tt.code)
			span := &mockReadOnlySpan{
				attributes: []attribute.KeyValue{
					attr,
				},
			}

			name := fmt.Sprintf("Should return %s given the code %d %s", tt.want, tt.code, how)

			t.Run(name, func(t *testing.T) {
				if got := utils.MapOtelStatus(span); got != tt.want {
					t.Errorf("MapOtelStatus() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("Given a GRPC Status code", func(t *testing.T) {
		tts := []struct {
			code    int
			factory func(key attribute.Key, i int) (attribute.KeyValue, string)
			want    sentry.SpanStatus
		}{
			{1, asInt, sentry.SpanStatusCanceled},
			{2, asInt, sentry.SpanStatusUnknown},
			{3, asInt, sentry.SpanStatusInvalidArgument},
			{4, asInt, sentry.SpanStatusDeadlineExceeded},
			{5, asInt, sentry.SpanStatusNotFound},
			{6, asInt, sentry.SpanStatusAlreadyExists},
			{7, asInt, sentry.SpanStatusPermissionDenied},
			{8, asInt, sentry.SpanStatusResourceExhausted},
			{9, asInt, sentry.SpanStatusFailedPrecondition},
			{10, asInt, sentry.SpanStatusAborted},
			{11, asInt, sentry.SpanStatusOutOfRange},
			{12, asInt, sentry.SpanStatusUnimplemented},
			{13, asInt, sentry.SpanStatusInternalError},
			{14, asInt, sentry.SpanStatusUnavailable},
			{15, asInt, sentry.SpanStatusDataLoss},
			{16, asInt, sentry.SpanStatusUnauthenticated},
			{1, asString, sentry.SpanStatusCanceled},
			{2, asString, sentry.SpanStatusUnknown},
			{3, asString, sentry.SpanStatusInvalidArgument},
			{4, asString, sentry.SpanStatusDeadlineExceeded},
			{5, asString, sentry.SpanStatusNotFound},
			{6, asString, sentry.SpanStatusAlreadyExists},
			{7, asString, sentry.SpanStatusPermissionDenied},
			{8, asString, sentry.SpanStatusResourceExhausted},
			{9, asString, sentry.SpanStatusFailedPrecondition},
			{10, asString, sentry.SpanStatusAborted},
			{11, asString, sentry.SpanStatusOutOfRange},
			{12, asString, sentry.SpanStatusUnimplemented},
			{13, asString, sentry.SpanStatusInternalError},
			{14, asString, sentry.SpanStatusUnavailable},
			{15, asString, sentry.SpanStatusDataLoss},
			{16, asString, sentry.SpanStatusUnauthenticated},
		}

		for _, tt := range tts {
			attr, how := tt.factory(semconv.RPCGRPCStatusCodeKey, tt.code)
			span := &mockReadOnlySpan{
				attributes: []attribute.KeyValue{
					attr,
				},
			}

			name := fmt.Sprintf("Should return %s given the code %d %s", tt.want, tt.code, how)
			t.Run(name, func(t *testing.T) {
				if got := utils.MapOtelStatus(span); got != tt.want {
					t.Errorf("MapOtelStatus() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}
