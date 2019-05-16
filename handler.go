package sentry

import (
	"context"
	"net/http"
)

// Decorate wraps `http.Handler` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func Decorate(handler http.Handler) http.Handler {
	hub := CurrentHub()

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = context.WithValue(ctx, ResponseContextKey, response)
		ctx = context.WithValue(ctx, RequestContextKey, request)
		ctx = SetHubOnContext(ctx, NewHub(hub.Client(), hub.Scope().Clone()))
		defer RecoverWithContext(ctx)
		handler.ServeHTTP(response, request.WithContext(ctx))
	})
}

// DecorateFunc wraps `http.HandlerFunc` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func DecorateFunc(handler http.HandlerFunc) http.HandlerFunc {
	hub := CurrentHub()

	return func(response http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = context.WithValue(ctx, ResponseContextKey, response)
		ctx = context.WithValue(ctx, RequestContextKey, request)
		ctx = SetHubOnContext(ctx, NewHub(hub.Client(), hub.Scope().Clone()))
		defer RecoverWithContext(ctx)
		handler(response, request.WithContext(ctx))
	}
}
