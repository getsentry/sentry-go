package sentry

import (
	"context"
	"net/http"
)

// Decorate wraps `http.Handler` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func Decorate(handler http.Handler) http.Handler {
	currentHub := CurrentHub()

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		client := currentHub.Client()
		scope := currentHub.Scope().Clone()
		scope.SetRequest(Request{}.FromHTTPRequest(request))
		hub := NewHub(client, scope)

		ctx := request.Context()
		ctx = context.WithValue(ctx, ResponseContextKey, response)
		ctx = context.WithValue(ctx, RequestContextKey, request)
		ctx = SetHubOnContext(ctx, hub)

		defer RecoverWithContext(ctx)

		handler.ServeHTTP(response, request.WithContext(ctx))
	})
}

// DecorateFunc wraps `http.HandlerFunc` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func DecorateFunc(handler http.HandlerFunc) http.HandlerFunc {
	currentHub := CurrentHub()

	return func(response http.ResponseWriter, request *http.Request) {
		client := currentHub.Client()
		scope := currentHub.Scope().Clone()
		scope.SetRequest(Request{}.FromHTTPRequest(request))
		hub := NewHub(client, scope)

		ctx := request.Context()
		ctx = context.WithValue(ctx, ResponseContextKey, response)
		ctx = context.WithValue(ctx, RequestContextKey, request)
		ctx = SetHubOnContext(ctx, hub)
		defer RecoverWithContext(ctx)

		handler(response, request.WithContext(ctx))
	}
}
