package sentrysql

import "github.com/getsentry/sentry-go"

// SpanOrigin identifies spans emitted by this package.
const SpanOrigin sentry.SpanOrigin = "auto.db.sentrysql"
