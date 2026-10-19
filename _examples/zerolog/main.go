package main

import (
	"context"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "",
		Environment:      "development",
		Release:          "1.0",
		Debug:            true,
		AttachStacktrace: true,
	})
	if err != nil {
		panic(err)
	}

	// The writer uses the client configured by sentry.Init.
	w, err := sentryzerolog.New(context.Background(), sentryzerolog.Options{
		Levels: []zerolog.Level{
			zerolog.DebugLevel,
			zerolog.ErrorLevel,
			zerolog.FatalLevel,
			zerolog.PanicLevel,
		},
		WithBreadcrumbs: true,
		FlushTimeout:    5 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	defer func() {
		err = w.Close()
		if err != nil {
			panic(err)
		}
	}()

	m := zerolog.MultiLevelWriter(os.Stdout, w)
	logger := zerolog.New(m).With().Timestamp().Logger()

	logger.Debug().Msg("Application has started")
	logger.Error().Msg("Oh no!")
	logger.Fatal().Msg("Can't continue...")
}
