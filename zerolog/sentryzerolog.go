package sentryzerolog

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/buger/jsonparser"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

// A large portion of this implementation has been taken from https://github.com/archdx/zerolog-sentry/blob/master/writer.go

var (
	// ErrFlushTimeout is returned when the flush operation times out.
	ErrFlushTimeout = errors.New("sentryzerolog flush timeout")

	// levels maps zerolog levels to sentry levels.
	levelsMapping = map[zerolog.Level]sentry.Level{
		zerolog.TraceLevel: sentry.LevelDebug,
		zerolog.DebugLevel: sentry.LevelDebug,
		zerolog.InfoLevel:  sentry.LevelInfo,
		zerolog.WarnLevel:  sentry.LevelWarning,
		zerolog.ErrorLevel: sentry.LevelError,
		zerolog.FatalLevel: sentry.LevelFatal,
		zerolog.PanicLevel: sentry.LevelFatal,
	}

	now = time.Now
)

// The identifier of the Zerolog SDK.
const sdkIdentifier = "sentry.go.zerolog"

// These default log field keys are used to pass specific metadata in a way that
// Sentry understands. If they are found in the log fields, and the value is of
// the expected datatype, it will be converted from a generic field, into Sentry
// metadata.
const (
	// FieldRequest holds an *http.Request.
	FieldRequest = "request"
	// FieldUser holds a User or *User value.
	FieldUser = "user"
	// FieldTransaction holds a transaction ID as a string.
	FieldTransaction = "transaction"
	// FieldFingerprint holds a string slice ([]string), used to dictate the
	// grouping of this event.
	FieldFingerprint = "fingerprint"

	// These fields are simply omitted, as they are duplicated by the Sentry SDK.
	FieldGoVersion = "go_version"
	FieldMaxProcs  = "go_maxprocs"

	// Name of the logger used by the Sentry SDK.
	logger = "zerolog"
)

type Config struct {
	sentry.ClientOptions
	Options
}

type Options struct {
	// Levels specifies the log levels that will trigger event sending to Sentry.
	// Only log messages at these levels will be sent. By default, the levels are
	// Error, Fatal, and Panic.
	Levels []zerolog.Level

	// WithBreadcrumbs, when enabled, adds log entries as breadcrumbs in Sentry.
	// Breadcrumbs provide a trail of events leading up to an error, which can
	// be invaluable for understanding the context of issues.
	WithBreadcrumbs bool

	// FlushTimeout sets the maximum duration allowed for flushing events to Sentry.
	// This is the time limit within which all pending events must be sent to Sentry
	// before the application exits. A typical use is ensuring all logs are sent before
	// application shutdown. The default timeout is usually 3 seconds.
	FlushTimeout time.Duration
}

func (o *Options) SetDefaults() {
	if len(o.Levels) == 0 {
		o.Levels = []zerolog.Level{
			zerolog.ErrorLevel,
			zerolog.FatalLevel,
			zerolog.PanicLevel,
		}
	}

	if o.FlushTimeout == 0 {
		o.FlushTimeout = 3 * time.Second
	}
}

// Deprecated: New simply invokes NewEventWriter.
func New(cfg Config) (Writer, error) {
	return NewEventWriter(cfg)
}

// Deprecated: NewWithHub simply invokes NewEventWriterWithHub.
func NewWithHub(hub *sentry.Hub, opts Options) (Writer, error) {
	return NewEventWriterWithHub(hub, opts)
}

// NewEventWriter allows for sending zerolog events to Sentry as events.
func NewEventWriter(cfg Config) (Writer, error) {
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

	return &eventWriter{
		hub:             sentry.NewHub(client, sentry.NewScope()),
		levels:          levels,
		flushTimeout:    cfg.FlushTimeout,
		withBreadcrumbs: cfg.WithBreadcrumbs,
	}, nil
}

// NewEventWriterWithHub allows for sending zerolog events to Sentry as events, using an existing sentry Hub and options.
func NewEventWriterWithHub(hub *sentry.Hub, opts Options) (Writer, error) {
	if hub == nil {
		return nil, errors.New("hub cannot be nil")
	}

	opts.SetDefaults()

	levels := make(map[zerolog.Level]struct{}, len(opts.Levels))
	for _, lvl := range opts.Levels {
		levels[lvl] = struct{}{}
	}

	return &eventWriter{
		hub:             hub,
		levels:          levels,
		flushTimeout:    opts.FlushTimeout,
		withBreadcrumbs: opts.WithBreadcrumbs,
	}, nil
}

// Writer is a sentry events writer with std io.Writer interface.
type Writer interface {
	Write(data []byte) (int, error)
	WriteLevel(level zerolog.Level, p []byte) (int, error)
	Close() error
}

type eventWriter struct {
	hub             *sentry.Hub
	levels          map[zerolog.Level]struct{}
	flushTimeout    time.Duration
	withBreadcrumbs bool
}

// addBreadcrumb adds event as a breadcrumb.
func (w *eventWriter) addBreadcrumb(event *sentry.Event) {
	if !w.withBreadcrumbs {
		return
	}

	breadcrumbType := "default"
	switch event.Level {
	case sentry.LevelFatal, sentry.LevelError:
		breadcrumbType = "error"
	}

	category, _ := event.Extra["category"].(string)

	w.hub.AddBreadcrumb(&sentry.Breadcrumb{
		Type:     breadcrumbType,
		Category: category,
		Message:  event.Message,
		Level:    event.Level,
		Data:     event.Extra,
	}, nil)
}

// Write handles zerolog's json and sends events to sentry.
func (w *eventWriter) Write(data []byte) (int, error) {
	n := len(data)

	lvl, err := parseLogLevel(data)
	if err != nil {
		return n, nil
	}

	event, ok := parseDataAsEvent(data)
	if !ok {
		return n, nil
	}

	event.Level, ok = levelsMapping[lvl]
	if !ok {
		return n, nil
	}

	if _, enabled := w.levels[lvl]; !enabled {
		// if the level is not enabled, add event as a breadcrumb
		w.addBreadcrumb(event)
		return n, nil
	}

	w.hub.CaptureEvent(event)
	// should flush before os.Exit
	if event.Level == sentry.LevelFatal {
		w.hub.Flush(w.flushTimeout)
	}

	return n, nil
}

func (w *eventWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	n := len(p)

	event, ok := parseDataAsEvent(p)
	if !ok {
		return n, nil
	}

	event.Level, ok = levelsMapping[level]
	if !ok {
		return n, nil
	}

	if _, enabled := w.levels[level]; !enabled {
		// if the level is not enabled, add event as a breadcrumb
		w.addBreadcrumb(event)
		return n, nil
	}

	w.hub.CaptureEvent(event)
	// should flush before os.Exit
	if event.Level == sentry.LevelFatal {
		w.hub.Flush(w.flushTimeout)
	}

	return n, nil
}

// Close forces client to flush all pending events.
// Can be useful before application exits.
func (w *eventWriter) Close() error {
	if ok := w.hub.Flush(w.flushTimeout); !ok {
		return ErrFlushTimeout
	}
	return nil
}

func parseLogLevel(data []byte) (zerolog.Level, error) {
	level, err := jsonparser.GetUnsafeString(data, zerolog.LevelFieldName)
	if err != nil {
		return zerolog.Disabled, nil
	}

	return zerolog.ParseLevel(level)
}

func parseDataAsEvent(data []byte) (*sentry.Event, bool) {
	event := sentry.Event{
		Timestamp: now(),
		Logger:    logger,
		Extra:     map[string]any{},
	}

	err := jsonparser.ObjectEach(data, func(key, value []byte, _ jsonparser.ValueType, _ int) error {
		k := string(key)
		switch k {
		case zerolog.MessageFieldName:
			event.Message = string(value)
		case zerolog.ErrorFieldName:
			event.Exception = append(event.Exception, sentry.Exception{
				Value:      string(value),
				Stacktrace: sentry.NewStacktrace(),
			})
		case zerolog.LevelFieldName, zerolog.TimestampFieldName:
			// Skip these as they're handled by Sentry
		case FieldUser:
			var user sentry.User
			err := json.Unmarshal(value, &user)
			if err != nil {
				event.Extra[k] = string(value)
			} else {
				event.User = user
			}
		case FieldTransaction:
			event.Transaction = string(value)
		case FieldFingerprint:
			var fp []string
			err := json.Unmarshal(value, &fp)
			if err != nil {
				event.Extra[k] = string(value)
			} else {
				event.Fingerprint = fp
			}
		case FieldGoVersion, FieldMaxProcs:
			// Skip these fields
		default:
			event.Extra[k] = string(value)
		}
		return nil
	})
	return &event, err == nil
}
