package sentryzerolog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger to provide the correct context to sentry.Logger.
type Logger struct {
	zerolog.Logger
	ctx          context.Context
	sentryLogger sentry.Logger
	levels       map[zerolog.Level]struct{}
	flushTimeout time.Duration
}

// NewLogger creates a new context-aware logger that automatically links log entries to traces.
func NewLogger(ctx context.Context, opts Options) *Logger {
	opts.SetDefaults()

	levels := make(map[zerolog.Level]struct{}, len(opts.Levels))
	for _, lvl := range opts.Levels {
		levels[lvl] = struct{}{}
	}

	cl := &Logger{
		ctx:          ctx,
		sentryLogger: sentry.NewLogger(ctx),
		levels:       levels,
		flushTimeout: opts.FlushTimeout,
	}

	// Create a custom writer that processes zerolog JSON and sends to sentry.Logger
	writer := &contextLogWriter{logger: cl}
	cl.Logger = zerolog.New(writer).With().Timestamp().Logger()

	return cl
}

// NewContextLoggerWithHub creates a new context-aware logger with an existing hub.
func NewContextLoggerWithHub(hub *sentry.Hub, ctx context.Context, opts Options) *Logger {
	opts.SetDefaults()

	levels := make(map[zerolog.Level]struct{}, len(opts.Levels))
	for _, lvl := range opts.Levels {
		levels[lvl] = struct{}{}
	}

	// Ensure hub is in context
	if sentry.GetHubFromContext(ctx) == nil {
		ctx = sentry.SetHubOnContext(ctx, hub)
	}

	logger := &Logger{
		ctx:          ctx,
		sentryLogger: sentry.NewLogger(ctx),
		levels:       levels,
		flushTimeout: opts.FlushTimeout,
	}

	// Create a custom writer that processes zerolog JSON and sends to sentry.Logger
	writer := &contextLogWriter{logger: logger}
	logger.Logger = zerolog.New(writer).With().Timestamp().Logger()

	return logger
}

// contextLogWriter processes zerolog JSON output and sends it to sentry.Logger
type contextLogWriter struct {
	logger *Logger
}

func (w *contextLogWriter) Write(p []byte) (int, error) {
	return w.WriteLevel(zerolog.DebugLevel, p)
}

func (w *contextLogWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	// Check if this log level is enabled
	if _, enabled := w.logger.levels[level]; !enabled {
		return len(p), nil
	}

	var evt map[string]any
	d := json.NewDecoder(bytes.NewReader(p))
	err := d.Decode(&evt)
	if err != nil {
		return 0, fmt.Errorf("cannot decode event: %w", err)
	}

	// Extract message and attributes
	var message string
	var attributes []attribute.Builder

	for k, value := range evt {
		switch k {
		case zerolog.LevelFieldName, zerolog.TimestampFieldName, FieldGoVersion, FieldMaxProcs:
			// fields that need to be skipped
		case zerolog.MessageFieldName:
			message, _ = value.(string)
		default:
			// Convert value to appropriate attribute
			switch val := value.(type) {
			case float64:
				if math.Trunc(val) > math.MaxInt64 {
					attributes = append(attributes, attribute.String(k, strconv.FormatUint(uint64(val), 10)))
				} else if val == float64(int64(val)) {
					attributes = append(attributes, attribute.Int64(k, int64(val)))
				} else {
					attributes = append(attributes, attribute.Float64(k, val))
				}
			case string:
				attributes = append(attributes, attribute.String(k, val))
			case bool:
				attributes = append(attributes, attribute.Bool(k, val))
			default:
				attributes = append(attributes, attribute.String(k, fmt.Sprint(value)))
			}
		}
	}

	if message == "" {
		message = string(p)
	}

	attributes = append(attributes, attribute.String("sentry.origin", zerologOrigin))

	// Set attributes on the sentry logger
	if len(attributes) > 0 {
		w.logger.sentryLogger.SetAttributes(attributes...)
	}

	ctx := w.logger.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	switch level {
	case zerolog.TraceLevel:
		w.logger.sentryLogger.Trace(ctx, message)
	case zerolog.DebugLevel:
		w.logger.sentryLogger.Debug(ctx, message)
	case zerolog.InfoLevel:
		w.logger.sentryLogger.Info(ctx, message)
	case zerolog.WarnLevel:
		w.logger.sentryLogger.Warn(ctx, message)
	case zerolog.ErrorLevel:
		w.logger.sentryLogger.Error(ctx, message)
	case zerolog.FatalLevel:
		w.logger.sentryLogger.Fatal(ctx, message)
	case zerolog.PanicLevel:
		w.logger.sentryLogger.Panic(ctx, message)
	}

	return len(p), nil
}

// WithContext returns a new Logger with the updated context.
func (cl *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		Logger:       cl.Logger,
		ctx:          ctx,
		sentryLogger: sentry.NewLogger(ctx),
		levels:       cl.levels,
		flushTimeout: cl.flushTimeout,
	}
}

// WithLevel returns a new event at the specified level with trace context.
func (cl *Logger) WithLevel(level zerolog.Level) *zerolog.Event {
	event := cl.Logger.WithLevel(level)
	return cl.injectTraceContext(event)
}

// Trace starts a new message with trace level and injects trace context.
func (cl *Logger) Trace() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Trace())
}

// Debug starts a new message with debug level and injects trace context.
func (cl *Logger) Debug() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Debug())
}

// Info starts a new message with info level and injects trace context.
func (cl *Logger) Info() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Info())
}

// Warn starts a new message with warn level and injects trace context.
func (cl *Logger) Warn() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Warn())
}

// Error starts a new message with error level and injects trace context.
func (cl *Logger) Error() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Error())
}

// Fatal starts a new message with fatal level and injects trace context.
func (cl *Logger) Fatal() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Fatal())
}

// Panic starts a new message with panic level and injects trace context.
func (cl *Logger) Panic() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Panic())
}

// Log starts a new message with no level and injects trace context.
func (cl *Logger) Log() *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Log())
}

// Err starts a new message with error level with err as a field if not nil.
func (cl *Logger) Err(err error) *zerolog.Event {
	return cl.injectTraceContext(cl.Logger.Err(err))
}

// With creates a child logger with additional fields.
func (cl *Logger) With() zerolog.Context {
	return cl.Logger.With()
}

// Sample returns a logger with the s sampler.
func (cl *Logger) Sample(s zerolog.Sampler) *Logger {
	return &Logger{
		Logger:       cl.Logger.Sample(s),
		ctx:          cl.ctx,
		sentryLogger: cl.sentryLogger,
		levels:       cl.levels,
		flushTimeout: cl.flushTimeout,
	}
}

// Hook returns a logger with the h Hook.
func (cl *Logger) Hook(h zerolog.Hook) *Logger {
	cl.Logger.Info()
	return &Logger{
		Logger:       cl.Logger.Hook(h),
		ctx:          cl.ctx,
		sentryLogger: cl.sentryLogger,
		levels:       cl.levels,
		flushTimeout: cl.flushTimeout,
	}
}

// Close flushes any pending logs.
func (cl *Logger) Close() error {
	// The sentry.Logger doesn't have a Close method, but we can flush the current hub
	if hub := sentry.GetHubFromContext(cl.ctx); hub != nil {
		if ok := hub.Flush(cl.flushTimeout); !ok {
			return ErrFlushTimeout
		}
	}
	return nil
}

// injectTraceContext extracts trace information from the current context and adds it to the log event.
func (cl *Logger) injectTraceContext(event *zerolog.Event) *zerolog.Event {
	if cl.ctx == nil {
		return event
	}

	// Try to get span from context
	if span := sentry.SpanFromContext(cl.ctx); span != nil {
		return event.
			Str("trace_id", span.TraceID.String()).
			Str("span_id", span.SpanID.String()).
			Str("parent_span_id", span.ParentSpanID.String())
	}

	// Try to get hub and propagation context
	if hub := sentry.GetHubFromContext(cl.ctx); hub != nil {
		scope := hub.Scope()
		if scope != nil {
			if scope.GetSpan() != nil {
				span := scope.GetSpan()
				return event.
					Str("trace_id", span.TraceID.String()).
					Str("span_id", span.SpanID.String()).
					Str("parent_span_id", span.ParentSpanID.String())
			}
		}
	}

	return event
}

// GetContext returns the current context associated with the logger.
func (cl *Logger) GetContext() context.Context {
	return cl.ctx
}
