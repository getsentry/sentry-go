package sentry_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	sentry "github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/google/go-cmp/cmp"
)

func TestSetTagAppliesToCorrectDerivedScope(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		parent := sentry.SetTag(f.NewContext(context.Background()), "parent", "1")

		sentry.CaptureMessage(parent, "parent")
		child := sentry.WithScope(parent, func(b *sentry.ScopeBuilder) {
			b.SetTag("child", "2")
		})
		sentry.CaptureMessage(child, "child")
		sibling := sentry.WithScope(parent, func(b *sentry.ScopeBuilder) {
			b.SetTag("sibling", "3")
		})
		sentry.CaptureMessage(sibling, "sibling")
		f.Flush()

		want := []*sentry.Event{
			{Message: "parent", Level: sentry.LevelInfo, Tags: map[string]string{"parent": "1"}},
			{Message: "child", Level: sentry.LevelInfo, Tags: map[string]string{"parent": "1", "child": "2"}},
			{Message: "sibling", Level: sentry.LevelInfo, Tags: map[string]string{"parent": "1", "sibling": "3"}},
		}
		got := f.Events()
		if diff := cmp.Diff(want, got, sentrytest.DefaultEventCmpOpts...); diff != "" {
			t.Fatalf("event mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestSetAPIsApplyToCapturedEvent(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := sentry.SetUser(f.NewContext(context.Background()), sentry.User{ID: "123", Email: "foo@example.com"})
		ctx = sentry.SetLevel(ctx, sentry.LevelWarning)
		ctx = sentry.SetFingerprint(ctx, []string{"fp-1", "fp-2"})
		ctx = sentry.SetContext(ctx, "runtime", sentry.Context{"name": "go"})
		ctx = sentry.AddBreadcrumb(ctx, &sentry.Breadcrumb{Message: "crumb"})

		sentry.CaptureMessage(ctx, "scoped")
		f.Flush()

		want := []*sentry.Event{{
			Message:     "scoped",
			Level:       sentry.LevelWarning,
			User:        sentry.User{ID: "123", Email: "foo@example.com"},
			Fingerprint: []string{"fp-1", "fp-2"},
			Contexts: map[string]sentry.Context{
				"runtime": {"name": "go"},
			},
			Breadcrumbs: []*sentry.Breadcrumb{{Message: "crumb"}},
		}}
		got := f.Events()

		if diff := cmp.Diff(want, got, sentrytest.DefaultEventCmpOpts); diff != "" {
			t.Fatalf("event mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestConfigureAppliesOnlyToDerivedCtx(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		parent := sentry.SetTag(f.NewContext(context.Background()), "parent", "1")

		child := sentry.WithScope(parent, func(b *sentry.ScopeBuilder) {
			b.SetTag("child", "2")
			b.SetUser(sentry.User{ID: "child-user"})
		})
		sentry.CaptureMessage(child, "child")
		sentry.CaptureMessage(parent, "parent")
		f.Flush()

		want := []*sentry.Event{
			{Message: "child", Level: sentry.LevelInfo, Tags: map[string]string{"parent": "1", "child": "2"}, User: sentry.User{ID: "child-user"}},
			{Message: "parent", Level: sentry.LevelInfo, Tags: map[string]string{"parent": "1"}},
		}
		got := f.Events()
		if diff := cmp.Diff(want, got, sentrytest.DefaultEventCmpOpts...); diff != "" {
			t.Fatalf("event mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestCtxScopeMutationRacesWhenScopeInternalsAreShared(t *testing.T) {
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		shared := sentry.SetTag(f.NewContext(context.Background()), "seed", "1")
		alias := shared

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			mutateScopeSetAPIs(alias, 1)
		}()
		go func() {
			defer wg.Done()
			mutateScopeSetAPIs(shared, 2)
		}()
		wg.Wait()
	})
}

func mutateScopeSetAPIs(ctx context.Context, x int) {
	ctx = sentry.SetTag(ctx, "shared", fmt.Sprintf("%d", x))
	ctx = sentry.SetUser(ctx, sentry.User{ID: fmt.Sprintf("u-%d", x)})
	ctx = sentry.SetRequest(ctx, httptest.NewRequest("GET", fmt.Sprintf("/%d", x), nil))
	ctx = sentry.SetRequestBody(ctx, []byte(fmt.Sprintf("body-%d", x)))
	ctx = sentry.SetAttributes(ctx, attribute.String("key", "value"))
	ctx = sentry.RemoveAttribute(ctx, "key")
	ctx = sentry.SetTags(ctx, map[string]string{"worker": fmt.Sprintf("%d", x)})
	ctx = sentry.SetContext(ctx, "worker", sentry.Context{"id": x})
	ctx = sentry.SetContexts(ctx, map[string]sentry.Context{"job": {"name": fmt.Sprintf("job-%d", x)}})
	ctx = sentry.SetFingerprint(ctx, []string{fmt.Sprintf("fp-%d", x)})
	ctx = sentry.SetLevel(ctx, sentry.LevelDebug)
	ctx = sentry.AddEventProcessor(ctx, func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event { return event })
	ctx = sentry.AddBreadcrumb(ctx, &sentry.Breadcrumb{Message: fmt.Sprintf("crumb-%d", x)})
	sentry.CaptureException(ctx, errExample(fmt.Sprintf("err-%d", x)))
}

type errExample string

func (e errExample) Error() string { return string(e) }
