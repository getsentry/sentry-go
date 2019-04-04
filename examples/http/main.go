package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sentry"
	"strconv"
	"time"
)

func prettyPrint(v interface{}) string {
	pp, _ := json.MarshalIndent(v, "", "  ")
	return string(pp)
}

type DevNullTransport struct{}

func (t *DevNullTransport) Configure(options sentry.ClientOptions) {
	dsn, _ := sentry.NewDsn(options.Dsn)
	fmt.Println()
	fmt.Println("Store Endpoint:", dsn.StoreAPIURL())
	fmt.Println("Headers:", dsn.RequestHeaders())
	fmt.Println()
}
func (t *DevNullTransport) SendEvent(event *sentry.Event) (*http.Response, error) {
	fmt.Println("Faked Transport")
	log.Println("Breadcrumbs:", prettyPrint(event.Breadcrumbs))
	return nil, nil
}

func customHandlerFunc(w http.ResponseWriter, r *http.Request) {
	if sentry.HasHubOnContext(r.Context()) {
		hub := sentry.GetHubFromContext(r.Context())
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #1 - " + strconv.Itoa(int(time.Now().Unix()))})
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "BreadcrumbFunc #2 - " + strconv.Itoa(int(time.Now().Unix()))})
	}

	panic("customHandlerFunc panicked")
}

type customHandler struct{}

func (th *customHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sentry.HasHubOnContext(r.Context()) {
		hub := sentry.GetHubFromContext(r.Context())
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "Breadcrumb #1 - " + strconv.Itoa(int(time.Now().Unix()))})
		hub.AddBreadcrumb(&sentry.Breadcrumb{Message: "Breadcrumb #2 - " + strconv.Itoa(int(time.Now().Unix()))})
	}

	panic("customHandler panicked")
}

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://whatever@sentry.io/1337",
		Transport: new(DevNullTransport),
	})

	if err != nil {
		panic(err)
	} else {
		fmt.Print("[Sentry] SDK initialized successfully\n\n")
	}

	http.Handle("/handle", sentry.Decorate(&customHandler{}))
	http.HandleFunc("/handlefunc", sentry.DecorateFunc(customHandlerFunc))

	log.Println("Please call me at localhost:8080/handle or localhost:8080/handlefunc")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
