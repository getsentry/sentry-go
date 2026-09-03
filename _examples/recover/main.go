package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func prettyPrint(v interface{}) string {
	pp, _ := json.MarshalIndent(v, "", "  ")
	return string(pp)
}

func fooErr() {
	barErr()
}

func barErr() {
	bazErr()
}

func bazErr() {
	panic(errors.New("sorry with error :("))
}

func fooMsg() {
	barMsg()
}

func barMsg() {
	bazMsg()
}

func bazMsg() {
	panic("sorry with message :(")
}

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Debug:            true,
		Dsn:              "https://hello@example.com/1337",
		AttachStacktrace: true,
		BeforeSend: func(e *sentry.Event, h *sentry.EventHint) *sentry.Event {
			fmt.Println(prettyPrint(e))
			return e
		},
	})

	scope := sentry.GlobalScope()
	scope.SetTag("oristhis", "justfantasy")
	scope.SetTag("isthis", "reallife")
	scope.SetLevel(sentry.LevelFatal)
	scope.SetUser(sentry.User{
		ID: "1337",
	})

	func() {
		defer sentry.Recover(context.Background(), nil)
		fooErr()
	}()

	func() {
		defer sentry.Recover(context.Background(), nil)
		fooMsg()
	}()

	sentry.Flush(time.Second * 5)
}
