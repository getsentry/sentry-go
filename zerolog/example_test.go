package sentryzerolog_test

import (
	"context"
	"os"

	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func ExampleNewSentryLogger() {
	// Assuming you're using the zerolog/log package:
	// import "github.com/rs/zerolog/log"

	log.Logger = zerolog.New(
		zerolog.MultiLevelWriter(zerolog.ConsoleWriter{Out: os.Stdout}, sentryzerolog.NewSentryLogger()),
	).With().Timestamp().Logger()

	log.Info().Msg("This is an info message")

	// You can populate context from *(net/http).Request.Context()
	// or any other context that has Sentry Hub on it.
	// The parent context will be respected and the event will be
	// linked to the parent event.
	ctx := context.Background()
	ctx = sentry.SetHubOnContext(ctx, sentry.CurrentHub().Clone())
	log.Error().
		Ctx(ctx).
		Err(os.ErrClosed).
		Str("file_name", "foo.txt").
		Msg("File does not exists")
}
