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
	f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
		Dsn: "https://public@example.com/1", Release: "enabled",
	}))
	ctx := f.NewContext(context.Background())
	trace := sentry.GetTraceparent(ctx)
	disabled := sentry.ContextWithClient(ctx, sentry.NewNoopClient())
	before, err := sentry.DynamicSamplingContextFromHeader([]byte(sentry.GetBaggage(disabled)))
	require.NoError(t, err)
	require.Empty(t, before.Entries)

	after, err := sentry.DynamicSamplingContextFromHeader([]byte(sentry.GetBaggage(ctx)))
	require.NoError(t, err)
	require.Equal(t, "public", after.Entries["public_key"])
	require.Equal(t, "enabled", after.Entries["release"])
	require.Equal(t, trace, sentry.GetTraceparent(ctx))
	require.NotNil(t, sentry.CaptureMessage(ctx, "after client binding"))
	f.Flush()
	require.Len(t, f.Events(), 1)
	require.Equal(t, after.Entries, f.Events()[0].GetDynamicSamplingContext())
}
