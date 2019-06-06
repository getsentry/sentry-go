package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

func prettyPrint(v interface{}) string {
	pp, _ := json.MarshalIndent(v, "", "  ")
	return string(pp)
}

type ctxKey int

const UserCtxKey = ctxKey(1337)

type devNullTransport struct{}

func (t *devNullTransport) Configure(options sentry.ClientOptions) {
	dsn, _ := sentry.NewDsn(options.Dsn)
	fmt.Println()
	fmt.Println("Store Endpoint:", dsn.StoreAPIURL())
	fmt.Println("Headers:", dsn.RequestHeaders())
	fmt.Println()
}
func (t *devNullTransport) SendEvent(event *sentry.Event) {
	fmt.Println("Faked Transport")
	log.Println(prettyPrint(event))
}

func (t *devNullTransport) Flush(timeout time.Duration) bool {
	return true
}

func customHandlerFunc(w http.ResponseWriter, r *http.Request) {
	if sentry.HasHubOnContext(r.Context()) {
		hub := sentry.GetHubFromContext(r.Context())
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #1 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #2 - " + strconv.Itoa(int(time.Now().Unix()))}, nil)
	}

	panic(errors.New("HTTPPanicHandler Error"))
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

	sentry.CaptureMessage("CaptureMessage")
	sentry.CaptureException(errors.New("CaptureMessage"))
	panic("HTTPPanicHandler Message")
}

type extractUser struct{}

func (eu extractUser) Name() string {
	return "extractUser"
}

func (eu extractUser) SetupOnce(client *sentry.Client) {
	client.AddEventProcessor(eu.processor)
}

func (eu extractUser) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
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
		Dsn:       "https://hello@world.io/1337",
		Transport: new(devNullTransport),
		Integrations: func(i []sentry.Integration) []sentry.Integration {
			return append(i, new(extractUser))
		},
	})

	if err != nil {
		panic(err)
	} else {
		fmt.Print("[Sentry] SDK initialized successfully\n\n")
	}

	sentryHandler := sentryhttp.New(sentryhttp.Options{
		Repanic:         true,
		WaitForDelivery: true,
	})

	http.Handle("/handle", sentryHandler.Handle(&customHandler{}))
	http.HandleFunc("/handlefunc", attachUser(sentryHandler.HandleFunc(customHandlerFunc)))

	log.Println("Please call me at localhost:8080/handle or localhost:8080/handlefunc")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
