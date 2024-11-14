package main

import (
	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
	"os"
	"time"
)

func main() {
	w, err := sentryzerolog.New(sentryzerolog.Config{
		Options: sentryzerolog.Options{
			Levels: []zerolog.Level{
				zerolog.DebugLevel,
				zerolog.ErrorLevel,
				zerolog.FatalLevel,
				zerolog.PanicLevel,
			},
			WithBreadcrumbs: true,
			FlushTimeout:    5 * time.Second,
		},
		ClientOptions: sentry.ClientOptions{
			Dsn:              "",
			Environment:      "development",
			Release:          "1.0",
			Debug:            true,
			AttachStacktrace: true,
		},
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
