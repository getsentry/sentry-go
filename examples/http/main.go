package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sentry"
	sentryIntegrations "sentry/integrations"
	"strconv"
	"time"
)

func prettyPrint(v interface{}) string {
	pp, _ := json.MarshalIndent(v, "", "  ")
	return string(pp)
}

type ctxKey int

const UserCtxKey = ctxKey(1337)

type DevNullTransport struct{}

func (t *DevNullTransport) Configure(options sentry.ClientOptions) {
	dsn, _ := sentry.NewDsn(options.Dsn)
	fmt.Println()
	fmt.Println("Store Endpoint:", dsn.StoreAPIURL())
	fmt.Println("Headers:", dsn.RequestHeaders())
	fmt.Println()
}
func (t *DevNullTransport) SendEvent(event *sentry.Event) {
	fmt.Println("Faked Transport")
	log.Println(prettyPrint(event))
}

func (t *DevNullTransport) Flush(timeout time.Duration) bool {
	return true
}

func customHandlerFunc(w http.ResponseWriter, r *http.Request) {
	if sentry.HasHubOnContext(r.Context()) {
		hub := sentry.GetHubFromContext(r.Context())
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #1 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #2 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
	}

	panic("customHandlerFunc panicked")
}

type User struct {
	id   int
	name string
}

func attachUser(handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		ctx = context.WithValue(ctx, UserCtxKey, User{
			id:   42,
			name: "PickleRick",
		})
		handler(response, request.WithContext(ctx))
	}
}

type customHandler struct{}

func (th *customHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sentry.HasHubOnContext(r.Context()) {
		hub := sentry.GetHubFromContext(r.Context())
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "Breadcrumb #1 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "Breadcrumb #2 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
	}

	panic("customHandler panicked")
}

type ExtractUser struct{}

func (eu ExtractUser) Name() string {
	return "ExtractUser"
}

func (eu ExtractUser) SetupOnce() {
	sentry.AddGlobalEventProcessor(eu.processor)
}

func (eu ExtractUser) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	// Run the integration only on the Client that registered it
	if sentry.CurrentHub().GetIntegration(eu.Name()) == nil {
		return event
	}

	if hint != nil && hint.Context != nil {
		if u, ok := hint.Context.Value(UserCtxKey).(User); ok {
			event.User = sentry.User{
				ID:       strconv.Itoa(u.id),
				Username: u.name,
			}
		}
	}

	return event
}

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://whatever@sentry.io/1337",
		Transport: new(DevNullTransport),
		Integrations: []sentry.Integration{
			new(ExtractUser),
			new(sentryIntegrations.RequestIntegration),
		},
	})

	if err != nil {
		panic(err)
	} else {
		fmt.Print("[Sentry] SDK initialized successfully\n\n")
	}

	http.Handle("/handle", sentry.Decorate(&customHandler{}))
	http.HandleFunc("/handlefunc", attachUser(sentry.DecorateFunc(customHandlerFunc)))

	log.Println("Please call me at localhost:8080/handle or localhost:8080/handlefunc")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
