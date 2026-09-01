package sentry

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

// The version of the SDK.
const SDKVersion = "0.49.0"

// apiVersion is the minimum version of the Sentry API compatible with the
// sentry-go SDK.
const apiVersion = "7"

// DefaultFlushTimeout is the default timeout used for flushing events.
const DefaultFlushTimeout = 2 * time.Second

// Init initializes the SDK with options. The returned error is non-nil if
// options is invalid, for instance if a malformed DSN is provided.
func Init(options ClientOptions) error {
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	GlobalScope().SetClient(client)
	return nil
}

// AddBreadcrumb records a new breadcrumb.
//
// The total number of breadcrumbs that can be recorded are limited by the
// configuration on the client.
func AddBreadcrumb(ctx context.Context, breadcrumb *Breadcrumb) {
	client := GetClient(ctx)
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
	scope := ScopeFromContext(ctx)
	if scope == nil {
		scope = GlobalScope()
	}
	scope.AddBreadcrumb(breadcrumb, limit)
}

func contextWithCaptureScope(ctx context.Context) context.Context {
	if ScopeFromContext(ctx) != nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return contextWithScope(ctx, GlobalScope())
}

// CaptureMessage captures an arbitrary message.
func CaptureMessage(ctx context.Context, message string, options ...CaptureOption) *EventID {
	ctx = contextWithCaptureScope(ctx)
	return GetClient(ctx).CaptureMessage(ctx, message, options...)
}

// CaptureException captures an error.
func CaptureException(ctx context.Context, err error, options ...CaptureOption) *EventID {
	ctx = contextWithCaptureScope(ctx)
	return GetClient(ctx).CaptureException(ctx, err, options...)
}

// CapturePanic captures a panic value returned by recover.
// It always attaches the stacktrace of the active panic.
func CapturePanic(ctx context.Context, recovered any, options ...CaptureOption) *EventID {
	ctx = contextWithCaptureScope(ctx)
	return GetClient(ctx).CapturePanic(ctx, recovered, options...)
}

// CaptureCheckIn captures a (cron) monitor check-in.
func CaptureCheckIn(ctx context.Context, checkIn *CheckIn, monitorConfig *MonitorConfig, options ...CaptureOption) *EventID {
	ctx = contextWithCaptureScope(ctx)
	return GetClient(ctx).CaptureCheckIn(ctx, checkIn, monitorConfig, options...)
}

// CaptureEvent captures an event on the currently active client if any.
//
// The event must already be assembled. Typically code would instead use
// the utility methods like CaptureException. The return value is the
// event ID. In case Sentry is disabled or event was dropped, the return value will be nil.
func CaptureEvent(ctx context.Context, event *Event, options ...CaptureOption) *EventID {
	ctx = contextWithCaptureScope(ctx)
	return GetClient(ctx).CaptureEvent(ctx, event, options...)
}

// Recover captures a panic from the current goroutine. It must be deferred.
func Recover(ctx context.Context, options ...CaptureOption) *EventID {
	return CapturePanic(ctx, recover(), options...)
}

// ConfigureScope updates the process-wide global scope.
func ConfigureScope(f func(scope *Scope)) {
	if f != nil {
		f(GlobalScope())
	}
}

// Flush waits until the underlying Transport sends any buffered events to the
// Sentry server, blocking for at most the given timeout. It returns false if
// the timeout was reached. In that case, some events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Do not call Flush indiscriminately after every call to CaptureEvent,
// CaptureException or CaptureMessage. Instead, to have the SDK send events over
// the network synchronously, configure it to use the HTTPSyncTransport in the
// call to Init.
func Flush(timeout time.Duration) bool {
	return GetClient(context.Background()).Flush(timeout)
}

// FlushWithContext waits until the underlying Transport sends any buffered events
// to the Sentry server, blocking for at most the duration specified by the context.
// It returns false if the context is canceled before the events are sent. In such a case,
// some events may not be delivered.
//
// FlushWithContext should be called before terminating the program to ensure no
// events are unintentionally dropped.
//
// Avoid calling FlushWithContext indiscriminately after each call to CaptureEvent,
// CaptureException, or CaptureMessage. To send events synchronously over the network,
// configure the SDK to use HTTPSyncTransport during initialization with Init.

func FlushWithContext(ctx context.Context) bool {
	return GetClient(ctx).FlushWithContext(ctx)
}

// LastEventID returns an ID of last captured event.
func LastEventID() EventID {
	return GlobalScope().LastEventID()
}
