// Package sentrylogrus provides a simple Logrus hook for Sentry.
package sentrylogrus

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	sentry "github.com/getsentry/sentry-go"
)

const (
	// sdkIdentifier is the identifier of the Logrus SDK.
	sdkIdentifier = "sentry.go.logrus"

	// the name of the logger
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

// Hook is the logrus hook for Sentry.
//
// It is not safe to configure the hook while logging is happening. Please
// perform all configuration before using it.
type Hook struct {
	hubProvider func() *sentry.Hub
	fallback    FallbackFunc
	keys        map[string]string
	levels      []logrus.Level
}

var _ logrus.Hook = &Hook{}

// New initializes a new Logrus hook which sends logs to a new Sentry client
// configured according to opts.
func New(levels []logrus.Level, opts sentry.ClientOptions) (*Hook, error) {
	client, err := sentry.NewClient(opts)
	if err != nil {
		return nil, err
	}

	client.SetSDKIdentifier(sdkIdentifier)

	return NewFromClient(levels, client), nil
}

// NewFromClient initializes a new Logrus hook which sends logs to the provided
// sentry client.
func NewFromClient(levels []logrus.Level, client *sentry.Client) *Hook {
	defaultHub := sentry.NewHub(client, sentry.NewScope())
	return &Hook{
		levels: levels,
		hubProvider: func() *sentry.Hub {
			// Default to using the same hub if no specific provider is set
			return defaultHub
		},
		keys: make(map[string]string),
	}
}

// SetHubProvider sets a function to provide a hub for each log entry.
// This can be used to ensure separate hubs per goroutine if needed.
func (h *Hook) SetHubProvider(provider func() *sentry.Hub) {
	h.hubProvider = provider
}

// AddTags adds tags to the hook's scope.
func (h *Hook) AddTags(tags map[string]string) {
	h.hubProvider().Scope().SetTags(tags)
}

// A FallbackFunc can be used to attempt to handle any errors in logging, before
// resorting to Logrus's standard error reporting.
type FallbackFunc func(*logrus.Entry) error

// SetFallback sets a fallback function, which will be called in case logging to
// sentry fails. In case of a logging failure in the Fire() method, the
// fallback function is called with the original logrus entry. If the
// fallback function returns nil, the error is considered handled. If it returns
// an error, that error is passed along to logrus as the return value from the
// Fire() call. If no fallback function is defined, a default error message is
// returned to Logrus in case of failure to send to Sentry.
func (h *Hook) SetFallback(fb FallbackFunc) {
	h.fallback = fb
}

// SetKey sets an alternate field key. Use this if the default values conflict
// with other loggers, for instance. You may pass "" for new, to unset an
// existing alternate.
func (h *Hook) SetKey(oldKey, newKey string) {
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

func (h *Hook) key(key string) string {
	if val := h.keys[key]; val != "" {
		return val
	}
	return key
}

// Levels returns the list of logging levels that will be sent to
// Sentry.
func (h *Hook) Levels() []logrus.Level {
	return h.levels
}

// Fire sends entry to Sentry.
func (h *Hook) Fire(entry *logrus.Entry) error {
	hub := h.hubProvider() // Use the hub provided by the HubProvider
	event := h.entryToEvent(entry)
	if id := hub.CaptureEvent(event); id == nil {
		if h.fallback != nil {
			return h.fallback(entry)
		}
		return errors.New("failed to send to sentry")
	}
	return nil
}

var levelMap = map[logrus.Level]sentry.Level{
	logrus.TraceLevel: sentry.LevelDebug,
	logrus.DebugLevel: sentry.LevelDebug,
	logrus.InfoLevel:  sentry.LevelInfo,
	logrus.WarnLevel:  sentry.LevelWarning,
	logrus.ErrorLevel: sentry.LevelError,
	logrus.FatalLevel: sentry.LevelFatal,
	logrus.PanicLevel: sentry.LevelFatal,
}

func (h *Hook) entryToEvent(l *logrus.Entry) *sentry.Event {
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

// Flush waits until the underlying Sentry transport sends any buffered events,
// blocking for at most the given timeout. It returns false if the timeout was
// reached, in which case some events may not have been sent.
func (h *Hook) Flush(timeout time.Duration) bool {
	return h.hubProvider().Client().Flush(timeout)
}

// FlushWithContext waits for the underlying Sentry transport to send any buffered
// events, blocking until the context's deadline is reached or the context is canceled.
// It returns false if the context is canceled or its deadline expires before the events
// are sent, meaning some events may not have been sent.
func (h *Hook) FlushWithContext(ctx context.Context) bool {
	return h.hubProvider().Client().FlushWithContext(ctx)
}
