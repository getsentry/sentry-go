package sentrylogrus

import (
	"github.com/sirupsen/logrus"

	"github.com/getsentry/sentry-go"
)

var DefaultContextExtractor ContextHubFunc = func(entry *logrus.Entry) *sentry.Hub {
	if ctx := entry.Context; ctx != nil {
		hub := sentry.GetHubFromContext(ctx)
		if hub != nil {
			return hub
		}
	}
	return nil
}
