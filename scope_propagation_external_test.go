package sentry_test

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/require"
)

func TestBaggageBeforeClientBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		incoming bool
		baggage  string
	}{
		{name: "local trace"},
		{name: "incoming DSC", incoming: true, baggage: "sentry-trace_id=11111111111111111111111111111111,sentry-public_key=upstream,sentry-release=upstream"},
		{name: "incoming frozen empty DSC", incoming: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
				Dsn: "https://public@example.com/1", Release: "enabled",
			}))
			ctx := f.NewContext(context.Background())
			if test.incoming {
				propagation, err := sentry.PropagationContextFromHeaders("11111111111111111111111111111111-2222222222222222-1", test.baggage)
				require.NoError(t, err)
				sentry.ScopeFromContext(ctx).SetPropagationContext(propagation)
			}
			trace := sentry.GetTraceparent(ctx)
			disabled := sentry.ContextWithClient(ctx, sentry.NewNoopClient())
			before, err := sentry.DynamicSamplingContextFromHeader([]byte(sentry.GetBaggage(disabled)))
			require.NoError(t, err)
			want, err := sentry.DynamicSamplingContextFromHeader([]byte(test.baggage))
			require.NoError(t, err)
			require.Equal(t, want.Entries, before.Entries)

			after, err := sentry.DynamicSamplingContextFromHeader([]byte(sentry.GetBaggage(ctx)))
			require.NoError(t, err)
			if test.incoming {
				require.Equal(t, want.Entries, after.Entries)
			} else {
				require.Equal(t, "public", after.Entries["public_key"])
				require.Equal(t, "enabled", after.Entries["release"])
			}
			require.Equal(t, trace, sentry.GetTraceparent(ctx))
			require.NotNil(t, sentry.CaptureMessage(ctx, "after client binding"))
			f.Flush()
			require.Len(t, f.Events(), 1)
			require.Len(t, f.Envelopes(), 1)
			require.Equal(t, after.Entries, f.Envelopes()[0].Header.Trace)
		})
	}
}
