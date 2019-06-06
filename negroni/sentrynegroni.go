package sentrynegroni

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go"
)

type SentryNegroni struct{}

func (sn *SentryNegroni) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	ctx := createContextWithHub(r)
	defer func() {
		if err := recover(); err != nil {
			sentry.GetHubFromContext(ctx).RecoverWithContext(ctx, err)
			panic(err)
		}
	}()
	next(rw, r.WithContext(ctx))
}

func createContextWithHub(r *http.Request) context.Context {
	parentHub := sentry.CurrentHub()
	client := parentHub.Client()
	scope := parentHub.Scope().Clone()
	isolatedHub := sentry.NewHub(client, scope)

	scope.SetRequest(sentry.Request{}.FromHTTPRequest(r))

	ctx := r.Context()
	ctx = context.WithValue(ctx, sentry.RequestContextKey, r)
	return sentry.SetHubOnContext(ctx, isolatedHub)
}

func New() *SentryNegroni {
	handler := SentryNegroni{}
	return &handler
}
