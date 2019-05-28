package sentry

import (
	"context"
	"net/http"
)

// Decorate wraps `http.Handler` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func Decorate(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := createContextWithHub(response, request)
		defer RecoverWithContext(ctx)
		handler.ServeHTTP(response, request.WithContext(ctx))
	})
}

// DecorateFunc wraps `http.HandlerFunc` and recovers from all the panics, providing necessary `Hub`
// instance that is bound to the request `Context` object.
func DecorateFunc(handler http.HandlerFunc) http.HandlerFunc {

	return func(response http.ResponseWriter, request *http.Request) {
		ctx := createContextWithHub(response, request)
		defer RecoverWithContext(ctx)
		handler(response, request.WithContext(ctx))
	}
}

func createContextWithHub(response http.ResponseWriter, request *http.Request) context.Context {
	client := CurrentHub().Client()
	scope := CurrentHub().Scope().Clone()
	scope.SetRequest(Request{}.FromHTTPRequest(request))

	ctx := request.Context()
	ctx = context.WithValue(ctx, ResponseContextKey, response)
	ctx = context.WithValue(ctx, RequestContextKey, request)

	return SetHubOnContext(ctx, NewHub(client, scope))
}
