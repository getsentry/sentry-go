package sentry

import (
	"net/http"
)

func Decorate(handler http.Handler) http.Handler {
	hub := GetGlobalHub()

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = SetHubOnContext(ctx, NewHub(hub.Client(), hub.Scope().Clone()))
		defer RecoverWithContext(ctx)
		handler.ServeHTTP(response, request.WithContext(ctx))
	})
}

func DecorateFunc(handler http.HandlerFunc) http.HandlerFunc {
	hub := GetGlobalHub()

	return func(response http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = SetHubOnContext(ctx, NewHub(hub.Client(), hub.Scope().Clone()))
		defer RecoverWithContext(ctx)
		handler(response, request.WithContext(ctx))
	}
}
