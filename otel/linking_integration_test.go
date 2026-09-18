package sentryotel_test

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentryotel "github.com/getsentry/sentry-go/otel"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func TestExternalSamplingPropagation(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, fixture *sentrytest.Fixture) {
		ctx := fixture.NewContext(context.Background())
		traceID, spanID := trace.TraceID{1}, trace.SpanID{2}
		ids := traceID.String() + "-" + spanID.String()
		for _, native := range []bool{false, true} {
			if native {
				root := sentry.StartTransaction(ctx, "native")
				defer root.Finish()
				ctx = root.Context()
			}
			for _, test := range []struct {
				name   string
				flags  trace.TraceFlags
				sentry string
				w3c    string
			}{
				{name: "sampled", flags: trace.FlagsSampled, sentry: "-1", w3c: "-01"},
				{name: "unsampled", sentry: "-0", w3c: "-00"},
			} {
				external := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: test.flags})
				linked := trace.ContextWithSpanContext(ctx, external)
				assert.Equal(t, ids+test.sentry, sentry.GetTraceparent(linked), "native=%t sampled=%s", native, test.name)
				assert.Equal(t, "00-"+ids+test.w3c, sentry.GetTraceparentW3C(linked), "native=%t sampled=%s", native, test.name)
			}
		}
	}, sentrytest.WithClientOptions(sentry.ClientOptions{
		EnableTracing: true, TracesSampleRate: 1,
		Integrations: func(in []sentry.Integration) []sentry.Integration {
			return append(in, sentryotel.NewOtelIntegration())
		},
	}))
}
