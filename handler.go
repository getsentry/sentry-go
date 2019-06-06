package sentry

import (
	"context"
	"net/http"
)

// Decorate wraps `http.Handler` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func Decorate(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ctx := createContextWithHub(r)
		defer func() {
			if err := recover(); err != nil {
				GetHubFromContext(ctx).RecoverWithContext(ctx, err)
				panic(err)
			}
		}()
		handler.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// DecorateFunc wraps `http.HandlerFunc` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func DecorateFunc(handler http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ctx := createContextWithHub(r)
		defer func() {
			if err := recover(); err != nil {
				GetHubFromContext(ctx).RecoverWithContext(ctx, err)
				panic(err)
			}
		}()
		handler(rw, r.WithContext(ctx))
	}
}

func createContextWithHub(r *http.Request) context.Context {
	parentHub := CurrentHub()
	client := parentHub.Client()
	scope := parentHub.Scope().Clone()
	isolatedHub := NewHub(client, scope)

	scope.SetRequest(Request{}.FromHTTPRequest(r))

	ctx := r.Context()
	ctx = context.WithValue(ctx, RequestContextKey, r)
	return SetHubOnContext(ctx, isolatedHub)
}
