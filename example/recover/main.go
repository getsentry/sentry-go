package main

import (
	"encoding/json"
	"errors"
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
	panic(errors.New("Sorry with error :("))
}

func fooMsg() {
	barMsg()
}

func barMsg() {
	bazMsg()
}

func bazMsg() {
	panic("Sorry with message :(")
}

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Debug: true,
		Dsn:   "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
	})

	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetExtra("oristhis", "justfantasy")
		scope.SetTag("isthis", "reallife")
		scope.SetLevel(sentry.LevelFatal)
		scope.SetUser(sentry.User{
			ID: "1337",
		})
	})

	func() {
		defer sentry.Recover()
		fooErr()
	}()

	func() {
		defer sentry.Recover()
		fooMsg()
	}()

	sentry.Flush(time.Second * 5)
}
