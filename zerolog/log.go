package sentryzerolog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/rs/zerolog"
	"math"
	"strconv"
	"time"
)

// NewLogWriter allows for sending zerolog events to Sentry as logs.
//
// If you want to send events/errors to Sentry, use the NewEventWriter() function instead.
func NewLogWriter(cfg Config) (Writer, error) {
	client, err := sentry.NewClient(cfg.ClientOptions)
	if err != nil {
		return nil, err
	}

	client.SetSDKIdentifier(sdkIdentifier)

	cfg.Options.SetDefaults()

	levels := make(map[zerolog.Level]struct{}, len(cfg.Levels))
	for _, lvl := range cfg.Levels {
		levels[lvl] = struct{}{}
	}

	hub := sentry.NewHub(client, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), hub)
	return &logWriter{
		ctx:          ctx,
		hub:          hub,
		levels:       levels,
		flushTimeout: cfg.FlushTimeout,
		logger:       sentry.NewLogger(ctx),
	}, nil
}

// NewLogWriterWithHub allows for sending zerolog events to Sentry as logs, using an existing sentry Hub and options.
//
// If you want to send events/errors to Sentry, use the NewEventWriter() function instead.
func NewLogWriterWithHub(hub *sentry.Hub, opts Options) (Writer, error) {
	if hub == nil || hub.Client() == nil {
		return nil, errors.New("hub or client cannot be nil")
	}
	hub.Client().SetSDKIdentifier(sdkIdentifier)
	opts.SetDefaults()

	levels := make(map[zerolog.Level]struct{}, len(opts.Levels))
	for _, lvl := range opts.Levels {
		levels[lvl] = struct{}{}
	}

	ctx := sentry.SetHubOnContext(context.Background(), hub)
	return &logWriter{
		ctx:          ctx,
		hub:          hub,
		levels:       levels,
		flushTimeout: opts.FlushTimeout,
		logger:       sentry.NewLogger(ctx),
	}, nil
}

type logWriter struct {
	ctx          context.Context
	hub          *sentry.Hub
	levels       map[zerolog.Level]struct{}
	flushTimeout time.Duration
	logger       sentry.Logger
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	return w.WriteLevel(zerolog.DebugLevel, p)
}

func (w *logWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	return w.writeAsLog(level, p)
}

// Close forces client to flush all pending events.
// Can be useful before application exits.
func (w *logWriter) Close() error {
	if ok := w.hub.Flush(w.flushTimeout); !ok {
		return ErrFlushTimeout
	}
	return nil
}

func (w *logWriter) writeAsLog(level zerolog.Level, p []byte) (n int, err error) {
	// Check if this log level is enabled
	if _, enabled := w.levels[level]; !enabled {
		// If level is not enabled, still return the number of bytes written
		// but don't process the log
		return len(p), nil
	}

	var evt map[string]any
	d := json.NewDecoder(bytes.NewReader(p))
	err = d.Decode(&evt)
	if err != nil {
		return 0, fmt.Errorf("cannot decode event: %w", err)
	}

	var message string
	for k, value := range evt {
		switch k {
		case zerolog.LevelFieldName, zerolog.TimestampFieldName:
		// Skip these as they're handled by Sentry
		case zerolog.MessageFieldName:
			message, _ = value.(string)
		case FieldUser:
			m, ok := value.(map[string]any)
			if !ok {
				w.logger.SetAttributes(attribute.String(FieldUser, fmt.Sprint(value)))
				continue
			}
			// converting to byte doesn't work, try to unmarshal manually.
			if id, ok := m["id"]; ok {
				w.logger.SetAttributes(attribute.String("user.id", fmt.Sprint(id)))
			}
			if email, ok := m["email"]; ok {
				w.logger.SetAttributes(attribute.String("user.email", fmt.Sprint(email)))
			}
			if name, ok := m["name"]; ok {
				w.logger.SetAttributes(attribute.String("user.name", fmt.Sprint(name)))
			}
		case zerolog.ErrorFieldName:
			w.logger.SetAttributes(attribute.String("error", fmt.Sprint(value)))
		case FieldGoVersion, FieldMaxProcs:
			// Skip these fields
		default:
			switch val := value.(type) {
			// json decode converts all numbers to float64, check if types satisfy int or float to convert properly.
			case float64:
				if math.Trunc(val) > math.MaxInt64 {
					w.logger.SetAttributes(attribute.String(k, strconv.FormatUint(uint64(val), 10)))
					continue
				}
				// check if value is integer
				if val == float64(int64(val)) {
					w.logger.SetAttributes(attribute.Int64(k, int64(val)))
				} else {
					w.logger.SetAttributes(attribute.Float64(k, val))
				}
			case string:
				w.logger.SetAttributes(attribute.String(k, val))
			case bool:
				w.logger.SetAttributes(attribute.Bool(k, val))
			default:
				// can't drop argument, fallback to string conversion
				w.logger.SetAttributes(attribute.String(k, fmt.Sprint(value)))
			}
		}
	}

	if message == "" {
		message = string(p)
	}

	w.logger.SetAttributes(attribute.String("sentry.origin", zerologOrigin))
	switch level {
	case zerolog.TraceLevel:
		w.logger.Trace(w.ctx, message)
	case zerolog.DebugLevel:
		w.logger.Debug(w.ctx, message)
	case zerolog.InfoLevel:
		w.logger.Info(w.ctx, message)
	case zerolog.WarnLevel:
		w.logger.Warn(w.ctx, message)
	case zerolog.ErrorLevel:
		w.logger.Error(w.ctx, message)
	case zerolog.FatalLevel:
		w.logger.Fatal(w.ctx, message)
	case zerolog.PanicLevel:
		w.logger.Panic(w.ctx, message)
	default:
		// skip on unknown level
	}

	// zerolog requires that the original number of bytes is returned.
	return len(p), nil
}
