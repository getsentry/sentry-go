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
		name      string
		capture   func(ctx context.Context, options ...sentry.CaptureOption)
		mechanism *sentry.Mechanism
		want      []*sentry.Mechanism
	}{
		{
			name: "exception",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			mechanism: &sentry.Mechanism{Type: "auto.test", Handled: new(true), Data: map[string]any{"key": "value"}},
			want:      []*sentry.Mechanism{{Type: "auto.test", Handled: new(true), Data: map[string]any{"key": "value"}}},
		},
		{
			name: "recovered panic",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.Recover(ctx, errBase, options...)
			},
			mechanism: &sentry.Mechanism{Type: "auto.test", Handled: new(false)},
			want:      []*sentry.Mechanism{{Type: "auto.test", Handled: new(false)}},
		},
		{
			name: "chain keeps chained mechanism and links",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, fmt.Errorf("wrapped: %w", errBase), options...)
			},
			mechanism: &sentry.Mechanism{Type: "auto.test", ExceptionID: 5, ParentID: new(3), Source: "ignored"},
			want: []*sentry.Mechanism{
				{Type: sentry.MechanismTypeChained, ExceptionID: 1, ParentID: new(0), Source: sentry.MechanismTypeUnwrap},
				{Type: "auto.test"},
			},
		},
		{
			name: "exception group",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errors.Join(errBase), options...)
			},
			mechanism: &sentry.Mechanism{Type: "auto.test"},
			want: []*sentry.Mechanism{
				{Type: sentry.MechanismTypeChained, ExceptionID: 1, ParentID: new(0), Source: "errors[0]"},
				{Type: "auto.test", IsExceptionGroup: true},
			},
		},
		{
			name: "empty type falls back to generic",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			mechanism: &sentry.Mechanism{Handled: new(false)},
			want:      []*sentry.Mechanism{{Type: sentry.MechanismTypeGeneric, Handled: new(false)}},
		},
		{
			name: "nil mechanism",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureException(ctx, errBase, options...)
			},
			want: []*sentry.Mechanism{nil},
		},
		{
			name: "message is unaffected",
			capture: func(ctx context.Context, options ...sentry.CaptureOption) {
				sentry.CaptureMessage(ctx, "message", options...)
			},
			mechanism: &sentry.Mechanism{Type: "auto.test"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var original sentry.Mechanism
			if test.mechanism != nil {
				original = *test.mechanism
			}
			var seenByBeforeSend []*sentry.Mechanism
			fixture := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
				BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					for _, exception := range event.Exception {
						seenByBeforeSend = append(seenByBeforeSend, exception.Mechanism)
					}
					return event
				},
			}))
			test.capture(fixture.Context, sentry.WithMechanism(test.mechanism))
			fixture.Flush()

			events := fixture.Events()
			require.Len(t, events, 1)
			var got []*sentry.Mechanism
			for _, exception := range events[0].Exception {
				got = append(got, exception.Mechanism)
			}
			require.Equal(t, test.want, got)
			require.Equal(t, test.want, seenByBeforeSend)
			if test.mechanism != nil {
				require.Equal(t, original, *test.mechanism, "the supplied mechanism must not be modified")
			}
		})
	}
}

func TestWithMechanismCopiesSharedMechanism(t *testing.T) {
	t.Parallel()
	shared := &sentry.Mechanism{Type: "auto.test", Handled: new(false), Data: map[string]any{"key": "value"}}
	fixture := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			mechanism := event.Exception[len(event.Exception)-1].Mechanism
			*mechanism.Handled = true
			mechanism.Data["key"] = "changed"
			return event
		},
	}))
	sentry.CaptureException(fixture.Context, errors.New("boom"), sentry.WithMechanism(shared))
	fixture.Flush()

	require.Len(t, fixture.Events(), 1)
	require.False(t, *shared.Handled)
	require.Equal(t, map[string]any{"key": "value"}, shared.Data)
}
