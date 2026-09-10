package sentry

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

// The version of the SDK.
const SDKVersion = "0.49.0"

// DefaultFlushTimeout is the default timeout used for flushing events.
const DefaultFlushTimeout = 2 * time.Second

// Init initializes the SDK with options. The returned error is non-nil if
// options is invalid, for instance if a malformed DSN is provided.
func Init(options ClientOptions) error {
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	setGlobalClient(client)
	return nil
}

// AddBreadcrumb records a new breadcrumb using the scope and client carried
// by ctx.
//
// The total number of breadcrumbs that can be recorded are limited by the
// configuration on the client.
func AddBreadcrumb(ctx context.Context, breadcrumb *Breadcrumb) {
	client := ClientFromContext(ctx)
	limit := client.options.MaxBreadcrumbs
	switch {
	case limit < 0:
		return
	case limit == 0:
		limit = defaultMaxBreadcrumbs
	}
	if client.options.BeforeBreadcrumb != nil {
		if breadcrumb = client.options.BeforeBreadcrumb(breadcrumb, &BreadcrumbHint{}); breadcrumb == nil {
			debuglog.Println("breadcrumb dropped due to BeforeBreadcrumb callback.")
			return
		}
	}
	scopeFromContextOrGlobal(ctx).AddBreadcrumb(breadcrumb, limit)
}

// CaptureMessage captures an arbitrary message using the scope and client
// carried by ctx.
func CaptureMessage(ctx context.Context, message string, options ...CaptureOption) *EventID {
	return ClientFromContext(ctx).CaptureMessage(ctx, message, options...)
}

// CaptureException captures an error using the scope and client carried by
// ctx.
func CaptureException(ctx context.Context, exception error, options ...CaptureOption) *EventID {
	return ClientFromContext(ctx).CaptureException(ctx, exception, options...)
}

// CaptureCheckIn captures a (cron) monitor check-in using the scope and client
// carried by ctx.
func CaptureCheckIn(
	ctx context.Context,
	checkIn *CheckIn,
	monitorConfig *MonitorConfig,
	options ...CaptureOption,
) *EventID {
	return ClientFromContext(ctx).CaptureCheckIn(ctx, checkIn, monitorConfig, options...)
}

// CaptureEvent captures an event using the scope and client carried by ctx.
//
// The event must already be assembled. Typically code would instead use
// the utility methods like CaptureException. The return value is the
// event ID. In case Sentry is disabled or event was dropped, the return value will be nil.
func CaptureEvent(ctx context.Context, event *Event, options ...CaptureOption) *EventID {
	return ClientFromContext(ctx).CaptureEvent(ctx, event, options...)
}

// Recover captures a recovered panic value using the scope and client carried
// by ctx. When recovered is nil, Recover invokes Go's built-in recover and must
// itself be deferred.
func Recover(ctx context.Context, recovered any, options ...CaptureOption) *EventID {
	if recovered == nil {
		recovered = recover()
	}
	if recovered == nil {
		return nil
	}
	return ClientFromContext(ctx).capturePanic(ctx, recovered, options...)
}

// Flush waits until the underlying Transport sends any buffered events to the
// Sentry server, blocking for at most the given timeout. It returns false if
// capture is disabled or the timeout was reached. In the latter case, some
// events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Capture remains asynchronous with every transport. Flush at shutdown or at
// the end of a serverless request when delivery must complete before returning.
func Flush(timeout time.Duration) bool {
	return ClientFromContext(context.Background()).Flush(timeout)
}

// FlushWithContext waits until the underlying Transport sends any buffered events
// to the Sentry server, blocking for at most the duration specified by the
// context. It returns false if capture is disabled or the context is canceled
// before the events are sent. In the latter case, some events may not be
// delivered.
//
// FlushWithContext should be called before terminating the program to ensure no
// events are unintentionally dropped.
//
// FlushWithContext flushes the client selected by ctx. Capture remains
// asynchronous even when using NewHTTPSyncTransport.
func FlushWithContext(ctx context.Context) bool {
	return ClientFromContext(ctx).FlushWithContext(ctx)
}

// LastEventID returns the last event ID captured in ctx's scope, or in the
// global scope when ctx does not carry one.
func LastEventID(ctx context.Context) EventID {
	return scopeFromContextOrGlobal(ctx).lastEventIDSnapshot()
}
