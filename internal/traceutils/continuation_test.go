package traceutils_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/traceutils"
	"github.com/stretchr/testify/require"
)

func TestContinueFromRequest(t *testing.T) {
	t.Parallel()
	traceID, parentID := sentry.TraceID{1}, sentry.SpanID{2}
	for _, test := range []struct {
		name, suffix string
		sampled      sentry.Sampled
		baggage      bool
	}{
		{name: "sampled with multiple baggage lines", suffix: "-1", sampled: sentry.SampledTrue, baggage: true},
		{name: "unsampled", suffix: "-0", sampled: sentry.SampledFalse},
		{name: "deferred"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set(sentry.SentryTraceHeader, traceID.String()+"-"+parentID.String()+test.suffix)
			if test.baggage {
				r.Header.Add(sentry.SentryBaggageHeader, "othervendor=value")
				r.Header.Add(sentry.SentryBaggageHeader, "sentry-trace_id="+traceID.String())
				r.Header.Add(sentry.SentryBaggageHeader, "sentry-public_key=upstream")
			}
			ctx, _ := sentry.WithIsolationScope(sentry.ContextWithClient(context.Background(), sentry.NewNoopClient()))
			root := sentry.StartTransaction(ctx, "request", traceutils.ContinueFromRequest(r))
			require.Equal(t, traceID, root.TraceID)
			require.Equal(t, parentID, root.ParentSpanID)
			require.Equal(t, test.sampled, root.Sampled)
			if test.baggage {
				baggage := sentry.GetBaggage(root.Context())
				require.Contains(t, baggage, "sentry-trace_id="+traceID.String())
				require.Contains(t, baggage, "sentry-public_key=upstream")
			}
			root.Finish()
		})
	}

	t.Run("active child ignores incoming parent", func(t *testing.T) {
		ctx, _ := sentry.WithIsolationScope(sentry.ContextWithClient(context.Background(), sentry.NewNoopClient()))
		root := sentry.StartTransaction(ctx, "root")
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set(sentry.SentryTraceHeader, traceID.String()+"-"+parentID.String()+"-1")
		child := sentry.StartSpan(root.Context(), "child", traceutils.ContinueFromRequest(r))
		require.Equal(t, root.TraceID, child.TraceID)
		require.Equal(t, root.SpanID, child.ParentSpanID)
		child.Finish()
		root.Finish()
	})
}
