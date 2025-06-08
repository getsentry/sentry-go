package sentryzerolog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/rs/zerolog"
)

// NewSentryLogger creates a new instance of SentryLogger that implements
// zerolog.LevelWriter interface. This should be used for sending zerolog
// events to Sentry as logs as opposed to events/errors.
//
// If you want to send events/errors to Sentry, use the New() function instead.
func NewSentryLogger() SentryLogger {
	return SentryLogger{}
}

// SentryLogger implements zerolog.LevelWriter.
// This should be used for sending zerolog events to Sentry as logs as opposed
// to events/errors.
type SentryLogger struct{}

var _ zerolog.LevelWriter = (*SentryLogger)(nil)
var _ io.Closer = (*SentryLogger)(nil)

func (s SentryLogger) Write(p []byte) (n int, err error) {
	return s.WriteLevel(zerolog.DebugLevel, p)
}

func (s SentryLogger) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	return s.runContext(context.Background(), level, p)
}

func (s SentryLogger) runContext(ctx context.Context, level zerolog.Level, p []byte) (n int, err error) {
	if !sentry.HasHubOnContext(ctx) {
		hub := sentry.CurrentHub()
		if hub == nil {
			hub = sentry.NewHub(nil, sentry.NewScope())
		}

		ctx = sentry.SetHubOnContext(context.Background(), hub)
	}

	var evt map[string]any
	d := json.NewDecoder(bytes.NewReader(p))
	err = d.Decode(&evt)
	if err != nil {
		return 0, fmt.Errorf("cannot decode event: %s", err.Error())
	}

	l := sentry.NewLogger(ctx)
	var message string
	for field, value := range evt {
		switch field {
		case zerolog.LevelFieldName:
			levelString, _ := value.(string)
			level, err = zerolog.ParseLevel(levelString)
			if err != nil {
				level = zerolog.DebugLevel
			}
		case zerolog.MessageFieldName:
			message, _ = value.(string)
		case zerolog.TimestampFieldName:
			continue
		default:
			switch valueType := value.(type) {
			case string:
				l.SetAttributes(attribute.String(field, valueType))
			case bool:
				l.SetAttributes(attribute.Bool(field, valueType))
			case float64:
				l.SetAttributes(attribute.Float64(field, float64(valueType)))
			case []any:
				for i, v := range valueType {
					switch vv := v.(type) {
					case string:
						l.SetAttributes(attribute.String(fmt.Sprintf("%s.%d", field, i), vv))
					case bool:
						l.SetAttributes(attribute.Bool(fmt.Sprintf("%s.%d", field, i), vv))
					case float64:
						l.SetAttributes(attribute.Float64(fmt.Sprintf("%s.%d", field, i), float64(vv)))
					}
				}
			}
		}
	}

	if message == "" {
		message = string(p)
	}

	switch level {
	case zerolog.TraceLevel:
		l.Trace(ctx, message)
	case zerolog.DebugLevel:
		l.Debug(ctx, message)
	case zerolog.InfoLevel:
		l.Info(ctx, message)
	case zerolog.WarnLevel:
		l.Warn(ctx, message)
	case zerolog.ErrorLevel:
		l.Error(ctx, message)
	case zerolog.FatalLevel:
		l.Fatal(ctx, message)
	case zerolog.PanicLevel:
		l.Panic(ctx, message)
	default:
		// for disabled level
		break
	}

	// Zerolog requires that the original number of bytes is returned.
	// Otherwise, it will return "short write" error.
	return len(p), nil
}

// Close should not be called directly.
// It should be called internally by zerolog.
func (s SentryLogger) Close() error {
	sentry.Flush(time.Second)
	return nil
}
