// Package sentrylogrus provides a simple Logrus hook for Sentry.
package sentrylogrus

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/sirupsen/logrus"
)

const (
	// sdkIdentifier is the identifier of the Logrus SDK.
	sdkIdentifier = "sentry.go.logrus"
	// the name of the logger.
	name = "logrus"
)

// These default log field keys are used to pass specific metadata in a way that
// Sentry understands. If they are found in the log fields, and the value is of
// the expected datatype, it will be converted from a generic field, into Sentry
// metadata.
//
// These keys may be overridden by calling SetKey on the hook object.
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
)

var levelMap = map[logrus.Level]sentry.Level{
	logrus.TraceLevel: sentry.LevelDebug,
	logrus.DebugLevel: sentry.LevelDebug,
	logrus.InfoLevel:  sentry.LevelInfo,
	logrus.WarnLevel:  sentry.LevelWarning,
	logrus.ErrorLevel: sentry.LevelError,
	logrus.FatalLevel: sentry.LevelFatal,
	logrus.PanicLevel: sentry.LevelFatal,
}

// Hook is the logrus hook for Sentry.
//
// It is not safe to configure the hook while logging is happening. Please
// perform all configuration before using it.
type Hook interface {
	// SetHubProvider sets a function to provide a hub for each log entry.
	SetHubProvider(provider func() *sentry.Hub)
	// AddTags adds tags to the hook's scope.
	AddTags(tags map[string]string)
	// SetFallback sets a fallback function for the eventHook.
	SetFallback(fb FallbackFunc)
	// SetKey sets an alternate field key for the eventHook.
	SetKey(oldKey, newKey string)
	// Levels returns the list of logging levels that will be sent to Sentry as events.
	Levels() []logrus.Level
	// Fire sends entry to Sentry as an event.
	Fire(entry *logrus.Entry) error
	// Flush waits until the underlying Sentry transport sends any buffered events.
	Flush(timeout time.Duration) bool
	// FlushWithContext waits for the underlying Sentry transport to send any buffered
	// events, blocking until the context's deadline is reached or the context is canceled.
	// It returns false if the context is canceled or its deadline expires before the events
	// are sent, meaning some events may not have been sent.
	FlushWithContext(ctx context.Context) bool
}

// Deprecated: New just makes an underlying call to NewEventHook.
func New(levels []logrus.Level, opts sentry.ClientOptions) (Hook, error) {
	return NewEventHook(levels, opts)
}

// Deprecated: NewFromClient just makes an underlying call to NewEventHookFromClient.
func NewFromClient(levels []logrus.Level, client *sentry.Client) Hook {
	return NewEventHookFromClient(levels, client)
}

// A FallbackFunc can be used to attempt to handle any errors in logging, before
// resorting to Logrus's standard error reporting.
type FallbackFunc func(*logrus.Entry) error

type eventHook struct {
	hubProvider func() *sentry.Hub
	fallback    FallbackFunc
	keys        map[string]string
	levels      []logrus.Level
}

var _ Hook = &eventHook{}
var _ logrus.Hook = &eventHook{} // eventHook still needs to be a logrus.Hook

func (h *eventHook) SetHubProvider(provider func() *sentry.Hub) {
	h.hubProvider = provider
}

func (h *eventHook) AddTags(tags map[string]string) {
	h.hubProvider().Scope().SetTags(tags)
}

func (h *eventHook) SetFallback(fb FallbackFunc) {
	h.fallback = fb
}

func (h *eventHook) SetKey(oldKey, newKey string) {
	if oldKey == "" {
		return
	}
	if newKey == "" {
		delete(h.keys, oldKey)
		return
	}
	delete(h.keys, newKey)
	h.keys[oldKey] = newKey
}

func (h *eventHook) key(key string) string {
	if val := h.keys[key]; val != "" {
		return val
	}
	return key
}

func (h *eventHook) Levels() []logrus.Level {
	return h.levels
}

func (h *eventHook) Fire(entry *logrus.Entry) error {
	hub := h.hubProvider()
	event := h.entryToEvent(entry)
	if id := hub.CaptureEvent(event); id == nil {
		if h.fallback != nil {
			return h.fallback(entry)
		}
		return errors.New("failed to send to sentry")
	}
	return nil
}

func (h *eventHook) entryToEvent(l *logrus.Entry) *sentry.Event {
	data := make(logrus.Fields, len(l.Data))
	for k, v := range l.Data {
		data[k] = v
	}
	s := &sentry.Event{
		Level:     levelMap[l.Level],
		Extra:     data,
		Message:   l.Message,
		Timestamp: l.Time,
		Logger:    name,
	}

	key := h.key(FieldRequest)
	switch request := s.Extra[key].(type) {
	case *http.Request:
		delete(s.Extra, key)
		s.Request = sentry.NewRequest(request)
	case sentry.Request:
		delete(s.Extra, key)
		s.Request = &request
	case *sentry.Request:
		delete(s.Extra, key)
		s.Request = request
	}

	if err, ok := s.Extra[logrus.ErrorKey].(error); ok {
		delete(s.Extra, logrus.ErrorKey)
		s.SetException(err, -1)
	}

	key = h.key(FieldUser)
	switch user := s.Extra[key].(type) {
	case sentry.User:
		delete(s.Extra, key)
		s.User = user
	case *sentry.User:
		delete(s.Extra, key)
		s.User = *user
	}

	key = h.key(FieldTransaction)
	if txn, ok := s.Extra[key].(string); ok {
		delete(s.Extra, key)
		s.Transaction = txn
	}

	key = h.key(FieldFingerprint)
	if fp, ok := s.Extra[key].([]string); ok {
		delete(s.Extra, key)
		s.Fingerprint = fp
	}

	delete(s.Extra, FieldGoVersion)
	delete(s.Extra, FieldMaxProcs)
	return s
}

func (h *eventHook) Flush(timeout time.Duration) bool {
	return h.hubProvider().Client().Flush(timeout)
}

func (h *eventHook) FlushWithContext(ctx context.Context) bool {
	return h.hubProvider().Client().FlushWithContext(ctx)
}

// NewEventHook initializes a new Logrus hook which sends events to a new Sentry client
// configured according to opts.
func NewEventHook(levels []logrus.Level, opts sentry.ClientOptions) (Hook, error) {
	client, err := sentry.NewClient(opts)
	if err != nil {
		return nil, err
	}

	client.SetSDKIdentifier(sdkIdentifier)
	return NewEventHookFromClient(levels, client), nil
}

// NewEventHookFromClient initializes a new Logrus hook which sends events to the provided
// sentry client.
func NewEventHookFromClient(levels []logrus.Level, client *sentry.Client) Hook {
	defaultHub := sentry.NewHub(client, sentry.NewScope())
	return &eventHook{
		levels: levels,
		hubProvider: func() *sentry.Hub {
			// Default to using the same hub if no specific provider is set
			return defaultHub
		},
		keys: make(map[string]string),
	}
}

type logHook struct {
	hubProvider func() *sentry.Hub
	fallback    FallbackFunc
	keys        map[string]string
	levels      []logrus.Level
	logger      sentry.Logger
}

var _ Hook = &logHook{}
var _ logrus.Hook = &logHook{} // logHook also needs to be a logrus.Hook

func (h *logHook) SetHubProvider(provider func() *sentry.Hub) {
	h.hubProvider = provider
}

func (h *logHook) AddTags(tags map[string]string) {
	// for logs convert tags to attributes
	for k, v := range tags {
		h.logger.SetAttributes(attribute.String(k, v))
	}
}

func (h *logHook) SetFallback(fb FallbackFunc) {
	h.fallback = fb
}

func (h *logHook) SetKey(oldKey, newKey string) {
	if oldKey == "" {
		return
	}
	if newKey == "" {
		delete(h.keys, oldKey)
		return
	}
	delete(h.keys, newKey)
	h.keys[oldKey] = newKey
}

func (h *logHook) key(key string) string {
	if val := h.keys[key]; val != "" {
		return val
	}
	return key
}

func (h *logHook) Fire(entry *logrus.Entry) error {
	ctx := context.Background()
	if entry.Context != nil {
		ctx = entry.Context
	}

	for k, v := range entry.Data {
		// Skip specific fields that might be handled separately
		if k == h.key(FieldRequest) || k == h.key(FieldUser) ||
			k == h.key(FieldFingerprint) || k == FieldGoVersion ||
			k == FieldMaxProcs || k == logrus.ErrorKey {
			continue
		}

		switch val := v.(type) {
		case int8:
			h.logger.SetAttributes(attribute.Int(k, int(val)))
		case int16:
			h.logger.SetAttributes(attribute.Int(k, int(val)))
		case int32:
			h.logger.SetAttributes(attribute.Int(k, int(val)))
		case int64:
			h.logger.SetAttributes(attribute.Int(k, int(val)))
		case int:
			h.logger.SetAttributes(attribute.Int(k, val))
		case uint, uint8, uint16, uint32, uint64:
			uval := reflect.ValueOf(val).Convert(reflect.TypeOf(uint64(0))).Uint()
			if uval <= math.MaxInt64 {
				h.logger.SetAttributes(attribute.Int64(k, int64(uval)))
			} else {
				// For values larger than int64 can handle, we are using string.
				h.logger.SetAttributes(attribute.String(k, strconv.FormatUint(uval, 10)))
			}
		case string:
			h.logger.SetAttributes(attribute.String(k, val))
		case float32:
			h.logger.SetAttributes(attribute.Float64(k, float64(val)))
		case float64:
			h.logger.SetAttributes(attribute.Float64(k, val))
		case bool:
			h.logger.SetAttributes(attribute.Bool(k, val))
		default:
			// can't drop argument, fallback to string conversion
			h.logger.SetAttributes(attribute.String(k, fmt.Sprint(v)))
		}
	}

	h.logger.SetAttributes(attribute.String("sentry.origin", "auto.logger.logrus"))

	switch entry.Level {
	case logrus.TraceLevel:
		h.logger.Trace(ctx, entry.Message)
	case logrus.DebugLevel:
		h.logger.Debug(ctx, entry.Message)
	case logrus.InfoLevel:
		h.logger.Info(ctx, entry.Message)
	case logrus.WarnLevel:
		h.logger.Warn(ctx, entry.Message)
	case logrus.ErrorLevel:
		h.logger.Error(ctx, entry.Message)
	case logrus.FatalLevel:
		h.logger.Fatal(ctx, entry.Message)
	case logrus.PanicLevel:
		h.logger.Panic(ctx, entry.Message)
	default:
		sentry.DebugLogger.Printf("Invalid logrus logging level: %v. Dropping log.", entry.Level)
		if h.fallback != nil {
			return h.fallback(entry)
		}
		return errors.New("invalid log level")
	}
	return nil
}

func (h *logHook) Levels() []logrus.Level {
	return h.levels
}

func (h *logHook) Flush(timeout time.Duration) bool {
	return h.hubProvider().Client().Flush(timeout)
}

func (h *logHook) FlushWithContext(ctx context.Context) bool {
	return h.hubProvider().Client().FlushWithContext(ctx)
}

// NewLogHook initializes a new Logrus hook which sends logs to a new Sentry client
// configured according to opts.
func NewLogHook(levels []logrus.Level, opts sentry.ClientOptions) (Hook, error) {
	if !opts.EnableLogs {
		return nil, errors.New("cannot create log hook, EnableLogs is set to false")
	}
	client, err := sentry.NewClient(opts)
	if err != nil {
		return nil, err
	}

	client.SetSDKIdentifier(sdkIdentifier)
	return NewLogHookFromClient(levels, client), nil
}

// NewLogHookFromClient initializes a new Logrus hook which sends logs to the provided
// sentry client.
func NewLogHookFromClient(levels []logrus.Level, client *sentry.Client) Hook {
	defaultHub := sentry.NewHub(client, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), defaultHub)
	return &logHook{
		logger: sentry.NewLogger(ctx),
		levels: levels,
		hubProvider: func() *sentry.Hub {
			// Default to using the same hub if no specific provider is set
			return defaultHub
		},
		keys: make(map[string]string),
	}
}
