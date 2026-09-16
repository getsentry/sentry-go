package sentrylogrus_test

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestContextProviderWithoutScopePreservesFallback(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		requestScope   bool
		providerClient bool
	}{
		{name: "construction scope"},
		{name: "construction scope with provider client", providerClient: true},
		{name: "request scope", requestScope: true},
		{name: "request scope with provider client", requestScope: true, providerClient: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := sentrytest.NewFixture(t)
			hook := sentrylogrus.NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, fixture.Client)
			var emissionCtx context.Context
			if test.requestScope {
				emissionCtx = fixture.NewContext(context.Background())
				sentry.ScopeFromContext(emissionCtx).SetAttributes(attribute.String("request.marker", "preserved"))
			}
			require.NoError(t, hook.Fire(&logrus.Entry{Context: emissionCtx, Level: logrus.InfoLevel, Message: "before provider"}))

			providerCtx := context.Background()
			if test.providerClient {
				providerCtx = sentry.ContextWithClient(providerCtx, fixture.Client)
			}
			hook.SetContextProvider(func() context.Context { return providerCtx })
			require.NoError(t, hook.Fire(&logrus.Entry{Context: emissionCtx, Level: logrus.InfoLevel, Message: "after provider"}))
			fixture.Flush()

			var logs []sentry.Log
			for _, event := range fixture.Events() {
				require.Equal(t, "log", event.Type)
				logs = append(logs, event.Logs...)
			}
			require.Len(t, logs, 2)
			require.NotEqual(t, sentry.TraceID{}, logs[0].TraceID)
			require.Equal(t, logs[0].TraceID, logs[1].TraceID)
			if test.requestScope {
				require.Equal(t, attribute.StringValue("preserved"), logs[1].Attributes["request.marker"])
			}
		})
	}
}
