package sentryzerolog

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A large portion of this implementation has been taken from https://github.com/archdx/zerolog-sentry/blob/master/writer_test.go

var logEventJSON = []byte(`{"level":"error","requestId":"bee07485-2485-4f64-99e1-d10165884ca7","error":"dial timeout","time":"2020-06-25T17:19:00+03:00","message":"test message"}`)

func TestParseLogEvent(t *testing.T) {
	ts := time.Now()

	now = func() time.Time { return ts }

	_, err := New(Config{})
	require.Nil(t, err)

	ev, ok := parseLogEvent(logEventJSON)
	require.True(t, ok)
	zLevel, err := parseLogLevel(logEventJSON)
	assert.Nil(t, err)
	ev.Level = levelsMapping[zLevel]

	assert.Equal(t, ts, ev.Timestamp)
	assert.Equal(t, sentry.LevelError, ev.Level)
	assert.Equal(t, "zerolog", ev.Logger)
	assert.Equal(t, "test message", ev.Message)

	require.Len(t, ev.Exception, 1)
	assert.Equal(t, "dial timeout", ev.Exception[0].Value)

	require.Len(t, ev.Extra, 1)
	assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", ev.Extra["requestId"])
}

func TestFailedClientCreation(t *testing.T) {
	_, err := New(Config{ClientOptions: sentry.ClientOptions{Dsn: "invalid"}})
	require.NotNil(t, err)
}

func TestNewWithHub(t *testing.T) {
	hub := sentry.CurrentHub()
	require.NotNil(t, hub)

	_, err := NewWithHub(hub, Options{
		Levels: []zerolog.Level{zerolog.ErrorLevel},
	})
	require.Nil(t, err)

	_, err = NewWithHub(nil, Options{})
	require.NotNil(t, err)
}

func TestParseLogLevel(t *testing.T) {
	_, err := New(Config{})
	require.Nil(t, err)

	level, err := parseLogLevel(logEventJSON)
	require.Nil(t, err)
	assert.Equal(t, zerolog.ErrorLevel, level)
}

func TestWrite(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				assert.Equal(t, sentry.LevelError, event.Level)
				assert.Equal(t, "test message", event.Message)
				require.Len(t, event.Exception, 1)
				assert.Equal(t, "dial timeout", event.Exception[0].Value)
				assert.True(t, time.Since(event.Timestamp).Minutes() < 1)
				assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", event.Extra["requestId"])
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			WithBreadcrumbs: true,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) {
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Interface("user", sentry.User{ID: "1", Email: "testuser@sentry.io"}).
		Strs("fingerprint", []string{"test"}).
		Logger()
	log.Err(errors.New("dial timeout")).
		Msg("test message")

	require.Nil(t, zerologError)
	require.True(t, beforeSendCalled)
}

func TestClose(t *testing.T) {
	cfg := Config{}
	writer, err := New(cfg)
	require.Nil(t, err)

	err = writer.Close()
	require.Nil(t, err)
}

func TestWrite_TraceDoesNotPanic(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			WithBreadcrumbs: false,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) {
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Str("user", "1").
		Str("transaction", "test").
		Str("fingerprint", "test").
		Logger()
	log.Trace().Msg("test message")

	require.Nil(t, zerologError)
	require.False(t, beforeSendCalled)
}

func TestWriteLevel(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				assert.Equal(t, sentry.LevelError, event.Level)
				assert.Equal(t, "test message", event.Message)
				require.Len(t, event.Exception, 1)
				assert.Equal(t, "dial timeout", event.Exception[0].Value)
				assert.True(t, time.Since(event.Timestamp).Minutes() < 1)
				assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", event.Extra["requestId"])
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			WithBreadcrumbs: true,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { zerologError = err }

	log := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(errors.New("dial timeout")).
		Msg("test message")

	require.Nil(t, zerologError)
	require.True(t, beforeSendCalled)
}

func TestWriteInvalidLevel(t *testing.T) {
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				assert.Equal(t, sentry.LevelError, event.Level)
				assert.Equal(t, "test message", event.Message)
				require.Len(t, event.Exception, 1)
				assert.Equal(t, "dial timeout", event.Exception[0].Value)
				assert.True(t, time.Since(event.Timestamp).Minutes() < 1)
				assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", event.Extra["requestId"])
				return event
			},
		},
		Options: Options{
			WithBreadcrumbs: true,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	log := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Log().Str("level", "invalid").Msg("test message")
}

func TestWrite_Disabled(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			Levels:          []zerolog.Level{zerolog.FatalLevel},
			WithBreadcrumbs: true,
		},
	}

	writer, err := New(cfg)

	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) {
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(errors.New("dial timeout")).
		Msg("test message")

	require.Nil(t, zerologError)
	require.False(t, beforeSendCalled)
}

func TestWriteLevel_Disabled(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			Levels:          []zerolog.Level{zerolog.FatalLevel},
			WithBreadcrumbs: true,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) {
		zerologError = err
	}

	log := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Str("go_version", "1.14").Str("go_max_procs", "4").
		Logger()
	log.Err(errors.New("dial timeout")).
		Msg("test message")

	require.Nil(t, zerologError)
	require.False(t, beforeSendCalled)
}

func TestWriteLevelFatal(t *testing.T) {
	var beforeSendCalled bool
	cfg := Config{
		ClientOptions: sentry.ClientOptions{
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				beforeSendCalled = true
				return event
			},
		},
		Options: Options{
			Levels:          []zerolog.Level{zerolog.FatalLevel},
			WithBreadcrumbs: true,
		},
	}
	writer, err := New(cfg)
	require.Nil(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) {
		zerologError = err
	}

	logger := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Str("go_version", "1.14").Str("go_max_procs", "4").Str("error", "dial timeout").Str("level", "fatal").Logger()

	logger.Log().Msg("test message")

	require.Nil(t, zerologError)
	require.False(t, beforeSendCalled)
}

func BenchmarkParseLogEvent(b *testing.B) {
	for i := 0; i < b.N; i++ {
		parseLogEvent(logEventJSON)
	}
}

func BenchmarkWriteLogEvent(b *testing.B) {
	w, err := New(Config{})
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.Write(logEventJSON)
	}
}

func BenchmarkWriteLogLevelEvent(b *testing.B) {
	w, err := New(Config{})
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.WriteLevel(zerolog.ErrorLevel, logEventJSON)
	}
}
