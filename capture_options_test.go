package sentry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/require"
)

func TestAddBreadcrumbHint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		options []sentry.BreadcrumbOption
		drop    bool
		write   bool
		want    sentry.BreadcrumbHint
	}{
		{name: "supplied", options: []sentry.BreadcrumbOption{sentry.WithBreadcrumbHint(&sentry.BreadcrumbHint{"source": "user"}), nil}, want: sentry.BreadcrumbHint{"source": "user"}},
		{name: "default", write: true, want: sentry.BreadcrumbHint{"callback": "wrote"}},
		{name: "nil hint", options: []sentry.BreadcrumbOption{sentry.WithBreadcrumbHint(nil)}, write: true, want: sentry.BreadcrumbHint{"callback": "wrote"}},
		{name: "dropped", options: []sentry.BreadcrumbOption{sentry.WithBreadcrumbHint(&sentry.BreadcrumbHint{"source": "user"})}, drop: true, want: sentry.BreadcrumbHint{"source": "user"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var gotHint sentry.BreadcrumbHint
			fixture := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
				BeforeBreadcrumb: func(breadcrumb *sentry.Breadcrumb, hint *sentry.BreadcrumbHint) *sentry.Breadcrumb {
					require.NotNil(t, hint)
					if test.write {
						(*hint)["callback"] = "wrote"
					}
					gotHint = *hint
					if test.drop {
						return nil
					}
					return breadcrumb
				},
			}))
			sentry.AddBreadcrumb(fixture.Context, &sentry.Breadcrumb{Message: "crumb"}, test.options...)
			require.NotNil(t, sentry.CaptureMessage(fixture.Context, "event"))
			fixture.Flush()
			require.Equal(t, test.want, gotHint)
			events := fixture.Events()
			require.Len(t, events, 1)
			if test.drop {
				require.Empty(t, events[0].Breadcrumbs)
			} else {
				require.Len(t, events[0].Breadcrumbs, 1)
			}
		})
	}
}

func TestWithMechanism(t *testing.T) {
	t.Parallel()
	errBase := errors.New("base")
	for _, test := range []struct {
		name    string
		capture func(ctx context.Context, options ...sentry.CaptureOption)
		options []sentry.CaptureOption
		want    []*sentry.Mechanism
	}{
		{
			name: "exception",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("auto.test", true)},
			want:    []*sentry.Mechanism{{Type: "auto.test", Handled: new(true)}},
		},
		{
			name: "recovered panic",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.Recover(ctx, errBase, options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("auto.test", false)},
			want:    []*sentry.Mechanism{{Type: "auto.test", Handled: new(false)}},
		},
		{
			name: "chain keeps chained mechanism and links",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, fmt.Errorf("wrapped: %w", errBase), options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("auto.test", true)},
			want: []*sentry.Mechanism{
				{Type: sentry.MechanismTypeChained, ExceptionID: 1, ParentID: new(0), Source: sentry.MechanismTypeUnwrap},
				{Type: "auto.test", Handled: new(true)},
			},
		},
		{
			name: "exception group",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errors.Join(errBase), options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("auto.test", true)},
			want: []*sentry.Mechanism{
				{Type: sentry.MechanismTypeChained, ExceptionID: 1, ParentID: new(0), Source: "errors[0]"},
				{Type: "auto.test", Handled: new(true), IsExceptionGroup: true},
			},
		},
		{
			name: "empty type falls back to generic",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("", false)},
			want:    []*sentry.Mechanism{{Type: sentry.MechanismTypeGeneric, Handled: new(false)}},
		},
		{
			name: "default mechanism",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			want: []*sentry.Mechanism{{Type: sentry.MechanismTypeGeneric, Handled: new(true)}},
		},
		{
			name: "message is unaffected",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureMessage(ctx, "message", options...)
			},
			options: []sentry.CaptureOption{sentry.WithMechanism("auto.test", true)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var seenByBeforeSend []*sentry.Mechanism
			fixture := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
				BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					for _, exception := range event.Exception {
						seenByBeforeSend = append(seenByBeforeSend, exception.Mechanism)
					}
					return event
				},
			}))
			test.capture(fixture.Context, test.options...)
			fixture.Flush()

			events := fixture.Events()
			require.Len(t, events, 1)
			var got []*sentry.Mechanism
			for _, exception := range events[0].Exception {
				got = append(got, exception.Mechanism)
			}
			require.Equal(t, test.want, got)
			require.Equal(t, test.want, seenByBeforeSend)
		})
	}
}
