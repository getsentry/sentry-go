package sentry_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSharedIsolation(t *testing.T) {
	t.Parallel()
	f := sentrytest.NewFixture(t)
	ctx, scope := sentry.WithIsolationScope(sentry.ContextWithClient(context.Background(), f.Client))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(x int) {
			defer wg.Done()
			touchScope(ctx, scope, x)
			scope.Clear()
			scope.Clone()
		}(i)
	}
	wg.Wait()
	f.Flush()
	require.Len(t, f.Events(), 20)
}

func touchScope(ctx context.Context, scope *sentry.Scope, x int) {
	scope.SetTag("foo", "bar")
	scope.SetContext("foo", sentry.Context{"foo": "bar"})
	scope.SetAttributes(attribute.String("foo", "bar"))
	scope.RemoveAttribute("foo")
	scope.SetLevel(sentry.LevelDebug)
	scope.SetFingerprint([]string{"foo"})
	scope.AddBreadcrumb(&sentry.Breadcrumb{Message: "foo"}, 100)
	scope.AddAttachment(&sentry.Attachment{Filename: "foo.txt"})
	scope.SetUser(sentry.User{ID: "foo"})
	scope.SetRequest(httptest.NewRequest("GET", "/foo", nil))
	scope.SetPropagationContext(sentry.NewPropagationContext())
	scope.SetSpan(&sentry.Span{TraceID: sentry.TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03")})

	sentry.CaptureException(ctx, fmt.Errorf("error %d", x))

	scope.ClearBreadcrumbs()
	scope.Clone()
}
