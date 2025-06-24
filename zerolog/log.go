package sentryzerolog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger and provides context-aware logging for Sentry integration.
// It maintains the same API as zerolog.Logger but ensures proper context handling for Sentry.
type Logger struct {
	zerolog.Logger
	writer Writer
	ctx    context.Context
}

// NewLogger creates a new context-aware Logger that wraps zerolog.Logger.
// It uses the provided Writer (typically a logWriter) for Sentry integration.
func NewLogger(writer Writer) Logger {
	return Logger{
		Logger: zerolog.New(writer),
		writer: writer,
		ctx:    context.Background(), // default context
	}
}

// WithContext returns a new Logger with the specified context.
// This context will be passed to the Sentry logger when events are written.
func (l Logger) WithContext(ctx context.Context) Logger {
	return Logger{
		Logger: l.Logger,
		writer: l.writer,
		ctx:    ctx,
	}
}

// With starts a new message with context fields
func (l Logger) With() zerolog.Context {
	return l.Logger.With()
}

// Level returns a new Logger with the specified level
func (l Logger) Level(level zerolog.Level) Logger {
	return Logger{
		Logger: l.Logger.Level(level),
		writer: l.writer,
		ctx:    l.ctx,
	}
}

// Sample returns a new Logger with sampling enabled
func (l Logger) Sample(s zerolog.Sampler) Logger {
	return Logger{
		Logger: l.Logger.Sample(s),
		writer: l.writer,
		ctx:    l.ctx,
	}
}

// Hook returns a new Logger with the specified hook
func (l Logger) Hook(hook zerolog.Hook) Logger {
	return Logger{
		Logger: l.Logger.Hook(hook),
		writer: l.writer,
		ctx:    l.ctx,
	}
}

// Output returns a new Logger with the specified output writer
func (l Logger) Output(w io.Writer) Logger {
	return Logger{
		Logger: l.Logger.Output(w),
		writer: l.writer,
		ctx:    l.ctx,
	}
}

// ContextAwareEvent wraps zerolog.Event and injects context when the event is sent
type ContextAwareEvent struct {
	*zerolog.Event
	logger Logger
}

// Msg sends the event with message to the logger with proper context
func (e *ContextAwareEvent) Msg(msg string) {
	if e.Event != nil {
		// Create a context-aware writer that uses the logger's context
		if lw, ok := e.logger.writer.(*logWriter); ok {
			// Temporarily update the logWriter's context
			originalCtx := lw.ctx
			lw.ctx = e.logger.ctx

			// Send the message
			e.Event.Msg(msg)

			// Restore original context
			lw.ctx = originalCtx
		} else {
			// Fallback for other writer types
			e.Event.Msg(msg)
		}
	}
}

// Msgf sends the event with formatted message to the logger with proper context
func (e *ContextAwareEvent) Msgf(format string, v ...interface{}) {
	if e.Event != nil {
		// Create a context-aware writer that uses the logger's context
		if lw, ok := e.logger.writer.(*logWriter); ok {
			// Temporarily update the logWriter's context
			originalCtx := lw.ctx
			lw.ctx = e.logger.ctx

			// Send the formatted message
			e.Event.Msgf(format, v...)

			// Restore original context
			lw.ctx = originalCtx
		} else {
			// Fallback for other writer types
			e.Event.Msgf(format, v...)
		}
	}
}

// Trace starts a new message with trace level
func (l Logger) Trace() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Trace(),
		logger: l,
	}
}

// Debug starts a new message with debug level
func (l Logger) Debug() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Debug(),
		logger: l,
	}
}

// Info starts a new message with info level
func (l Logger) Info() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Info(),
		logger: l,
	}
}

// Warn starts a new message with warn level
func (l Logger) Warn() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Warn(),
		logger: l,
	}
}

// Error starts a new message with error level
func (l Logger) Error() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Error(),
		logger: l,
	}
}

// Err starts a new message with error level and adds the error as a field
func (l Logger) Err(err error) *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Err(err),
		logger: l,
	}
}

// Fatal starts a new message with fatal level
func (l Logger) Fatal() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Fatal(),
		logger: l,
	}
}

// Panic starts a new message with panic level
func (l Logger) Panic() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Panic(),
		logger: l,
	}
}

// Log starts a new message with no level
func (l Logger) Log() *ContextAwareEvent {
	return &ContextAwareEvent{
		Event:  l.Logger.Log(),
		logger: l,
	}
}

// Print sends a log event using debug level and no extra field
func (l Logger) Print(v ...interface{}) {
	l.Debug().Msg(fmt.Sprint(v...))
}

// Printf sends a log event using debug level and no extra field
func (l Logger) Printf(format string, v ...interface{}) {
	l.Debug().Msgf(format, v...)
}

// Write implements io.Writer interface
func (l Logger) Write(p []byte) (n int, err error) {
	return l.Logger.Write(p)
}

// WriteLevel writes a message at the specified level
func (l Logger) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	if lw, ok := l.writer.(*logWriter); ok {
		// Use context-aware WriteLevel
		originalCtx := lw.ctx
		lw.ctx = l.ctx
		defer func() { lw.ctx = originalCtx }()

		return lw.WriteLevel(level, p)
	}

	// Fallback for other writer types
	if wl, ok := l.writer.(interface {
		WriteLevel(zerolog.Level, []byte) (int, error)
	}); ok {
		return wl.WriteLevel(level, p)
	}

	return l.writer.Write(p)
}

// GetLevel returns the current logging level
func (l Logger) GetLevel() zerolog.Level {
	return l.Logger.GetLevel()
}

// UpdateContext updates the logger's internal context and returns a new logger
func (l Logger) UpdateContext(update func(c zerolog.Context) zerolog.Context) Logger {
	l.Logger.UpdateContext(update)
	return Logger{
		Logger: l.Logger,
		writer: l.writer,
		ctx:    l.ctx,
	}
}

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
	return w.writeAsLog(w.ctx, level, p)
}

// Close forces client to flush all pending events.
// Can be useful before application exits.
func (w *logWriter) Close() error {
	if ok := w.hub.Flush(w.flushTimeout); !ok {
		return ErrFlushTimeout
	}
	return nil
}

func (w *logWriter) writeAsLog(ctx context.Context, level zerolog.Level, p []byte) (n int, err error) {
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
	logger := sentry.NewLogger(ctx)

	for k, value := range evt {
		switch k {
		case zerolog.LevelFieldName, zerolog.TimestampFieldName, FieldGoVersion, FieldMaxProcs:
			// fields that need to be skipped
		case zerolog.MessageFieldName:
			message, _ = value.(string)
		default:
			switch val := value.(type) {
			// json decode converts all numbers to float64, check if types satisfy int or float to convert properly.
			case float64:
				if math.Trunc(val) > math.MaxInt64 {
					logger.SetAttributes(attribute.String(k, strconv.FormatUint(uint64(val), 10)))
					continue
				}
				// check if value is integer
				if val == float64(int64(val)) {
					logger.SetAttributes(attribute.Int64(k, int64(val)))
				} else {
					logger.SetAttributes(attribute.Float64(k, val))
				}
			case string:
				logger.SetAttributes(attribute.String(k, val))
			case bool:
				logger.SetAttributes(attribute.Bool(k, val))
			default:
				// can't drop argument, fallback to string conversion
				logger.SetAttributes(attribute.String(k, fmt.Sprint(value)))
			}
		}
	}

	if message == "" {
		message = string(p)
	}

	logger.SetAttributes(attribute.String("sentry.origin", zerologOrigin))
	switch level {
	case zerolog.TraceLevel:
		logger.Trace(ctx, message)
	case zerolog.DebugLevel:
		logger.Debug(ctx, message)
	case zerolog.InfoLevel:
		logger.Info(ctx, message)
	case zerolog.WarnLevel:
		logger.Warn(ctx, message)
	case zerolog.ErrorLevel:
		logger.Error(ctx, message)
	case zerolog.FatalLevel:
		logger.Fatal(ctx, message)
	case zerolog.PanicLevel:
		logger.Panic(ctx, message)
	default:
		// skip on unknown level
	}

	// zerolog requires that the original number of bytes is returned.
	return len(p), nil
}
