package main

import (
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "https://definitelyincorrect@oiasaskjd.io/42",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			return event
		},
	})

	for i := 1; i < 20; i++ {
		sentry.CaptureMessage(fmt.Sprintf("Event #%d", i))
		time.Sleep(time.Millisecond * 1000)
	}

	fmt.Println("=> Flushing transport buffer")
	if sentry.Flush(time.Second * 2) {
		fmt.Println("=> All queued events delivered!")
	} else {
		fmt.Println("=> Flush timeout reached")
	}

	time.Sleep(time.Second * 100)
}
