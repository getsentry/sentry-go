//go:build legacy

package prreview

import sentry "github.com/getsentry/sentry-go"

func newSyncTransport() sentry.Transport {
	return sentry.NewHTTPSyncTransport()
}
