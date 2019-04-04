package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sentry"
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
	return nil, nil
}

func recoverHandler() {
	defer sentry.Recover()
	panic("ups")
}

func beforeSend() {
	sentry.CaptureMessage("Drop me!")
}

func captureMessage() {
	sentry.CaptureMessage("say what again. SAY WHAT again")
}

func configureScope() {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetExtra("oristhis", "justfantasy")
		scope.SetTag("isthis", "reallife")
		scope.SetLevel(sentry.LevelFatal)
		scope.SetUser(sentry.User{
			ID: "1337",
		})
	})
}

func withScope() {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelFatal)
		sentry.CaptureException(errors.New("say what again. SAY WHAT again"))
	})
}

func addBreadcrumbs() {
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Message: "Random breadcrumb 1",
	})

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Message: "Random breadcrumb 2",
	})

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Message: "Random breadcrumb 3",
	})
}

func withScopeAndConfigureScope() {
	sentry.WithScope(func(scope *sentry.Scope) {
		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetExtras(map[string]interface{}{
				"istillcant": 42,
				"believe":    "that",
			})
			scope.SetTags(map[string]string{
				"italready": "works",
				"just":      "likethat",
			})
		})

		sentry.CaptureEvent(&sentry.Event{
			Message: "say what again. SAY WHAT again",
		})
	})
}

func main() {
	// Init
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "https://14830a963b1e4c20ad90e47289c1fe98@sentry.io/1419836",
		BeforeSend: func(event *sentry.Event) *sentry.Event {
			if event.Message == "Drop me!" {
				return nil
			}
			fmt.Printf("%s\n\n", prettyPrint(event))
			return event
		},
		SampleRate: 0.99,
		Transport:  new(DevNullTransport),
	})

	if err != nil {
		panic(err)
	} else {
		fmt.Print("[Sentry] SDK initialized successfully\n\n")
	}

	beforeSend()
	configureScope()
	withScope()
	captureMessage()
	addBreadcrumbs()
	withScopeAndConfigureScope()
	recoverHandler()
}
