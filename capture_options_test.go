package sentry_test

import (
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
