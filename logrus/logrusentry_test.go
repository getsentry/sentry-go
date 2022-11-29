package sentrylogrus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pkgerr "github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/getsentry/sentry-go"
)

const testDSN = "http://test:test@localhost/1234"

type testResponder func(*http.Request) (*http.Response, error)

func (t testResponder) RoundTrip(r *http.Request) (*http.Response, error) {
	return t(r)
}

func xport(req *http.Request) http.RoundTripper {
	return testResponder(func(r *http.Request) (*http.Response, error) {
		*req = *r
		return &http.Response{}, nil
	})
}

func TestNew(t *testing.T) {
	t.Parallel()
	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()
		_, err := New(nil, sentry.ClientOptions{Dsn: "%xxx"})
		if err == nil || !strings.Contains(err.Error(), "invalid URL escape") {
			t.Errorf("Unexpected error: %s", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		req := new(http.Request)
		h, err := New(nil, sentry.ClientOptions{
			Dsn:           testDSN,
			HTTPTransport: xport(req),
		})
		if err != nil {
			t.Fatal(err)
		}
		if id := h.hub.CaptureEvent(&sentry.Event{}); id == nil {
			t.Error("CaptureEvent failed")
		}
		if !h.Flush(5 * time.Second) {
			t.Error("flush failed")
		}
		testEvent(t, req.Body, map[string]interface{}{
			"level": "info",
		})
	})
}

func TestFire(t *testing.T) {
	t.Parallel()

	entry := &logrus.Entry{
		Level: logrus.ErrorLevel,
	}

	req := new(http.Request)
	opts := sentry.ClientOptions{}
	opts.Dsn = testDSN
	opts.HTTPTransport = xport(req)
	hook, err := New([]logrus.Level{logrus.ErrorLevel}, opts)
	if err != nil {
		t.Fatal(err)
	}
	err = hook.Fire(entry)
	if err != nil {
		t.Fatal(err)
	}

	if !hook.Flush(5 * time.Second) {
		t.Error("flush failed")
	}
	testEvent(t, req.Body, map[string]interface{}{
		"level": "error",
	})
}

func Test_e2e(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		levels  []logrus.Level
		opts    sentry.ClientOptions
		init    func(*Hook)
		log     func(*logrus.Logger)
		skipped bool
		want    map[string]interface{}
	}{
		{
			name:   "skip info",
			levels: []logrus.Level{logrus.ErrorLevel},
			log: func(l *logrus.Logger) {
				l.Info("foo")
			},
			skipped: true,
		},
		{
			name:   "error level",
			levels: []logrus.Level{logrus.ErrorLevel},
			log: func(l *logrus.Logger) {
				l.Error("foo")
			},
			want: map[string]interface{}{
				"level":   "error",
				"message": "foo",
			},
		},
		{
			name:   "metadata",
			levels: []logrus.Level{logrus.ErrorLevel},
			opts: sentry.ClientOptions{
				Environment: "production",
				ServerName:  "localhost",
				Release:     "v1.2.3",
				Dist:        "beta",
			},
			log: func(l *logrus.Logger) {
				l.Error("foo")
			},
			want: map[string]interface{}{
				"dist":        "beta",
				"environment": "production",
				"level":       "error",
				"message":     "foo",
			},
		},
		{
			name:   "tags",
			levels: []logrus.Level{logrus.ErrorLevel},
			opts: sentry.ClientOptions{
				AttachStacktrace: true,
			},
			init: func(h *Hook) {
				h.AddTags(map[string]string{
					"foo": "bar",
				})
			},
			log: func(l *logrus.Logger) {
				l.Error("foo")
			},
			want: map[string]interface{}{
				"level":   "error",
				"message": "foo",
				"tags":    map[string]interface{}{"foo": "bar"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := new(http.Request)
			l := logrus.New()
			opts := tt.opts
			opts.Dsn = testDSN
			opts.HTTPTransport = xport(req)
			hook, err := New(tt.levels, opts)
			if err != nil {
				t.Fatal(err)
			}
			if init := tt.init; init != nil {
				init(hook)
			}
			l.SetOutput(io.Discard)
			l.AddHook(hook)
			tt.log(l)

			if !hook.Flush(5 * time.Second) {
				t.Fatal("failed to flush")
			}
			if tt.skipped {
				if req.Method != "" {
					t.Error("Got an unexpected request")
				}
				return
			}
			testEvent(t, req.Body, tt.want)
		})
	}
}

func testEvent(t *testing.T, r io.ReadCloser, want map[string]interface{}) {
	t.Helper()
	t.Cleanup(func() {
		_ = r.Close()
	})
	var event map[string]interface{}
	if err := json.NewDecoder(r).Decode(&event); err != nil {
		t.Fatal(err)
	}
	// delete static or non-deterministic fields
	for _, k := range []string{"timestamp", "event_id", "contexts", "release", "server_name", "sdk", "platform", "user", "modules"} {
		delete(event, k)
	}
	if d := cmp.Diff(want, event); d != "" {
		t.Error(d)
	}
}

func Test_entry2event(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry *logrus.Entry
		want  *sentry.Event
	}{
		{
			name:  "empty entry",
			entry: &logrus.Entry{},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
			},
		},
		{
			name: "data fields",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					"foo": 123.4,
					"bar": "oink",
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{"bar": "oink", "foo": 123.4},
			},
		},
		{
			name: "info level",
			entry: &logrus.Entry{
				Level: logrus.InfoLevel,
			},
			want: &sentry.Event{
				Level: "info",
				Extra: map[string]interface{}{},
			},
		},
		{
			name: "message",
			entry: &logrus.Entry{
				Message: "the only thing we have to fear is fear itself",
			},
			want: &sentry.Event{
				Level:   "fatal",
				Extra:   map[string]interface{}{},
				Message: "the only thing we have to fear is fear itself",
			},
		},
		{
			name: "timestamp",
			entry: &logrus.Entry{
				Time: time.Unix(1, 2).UTC(),
			},
			want: &sentry.Event{
				Level:     "fatal",
				Extra:     map[string]interface{}{},
				Timestamp: time.Unix(1, 2).UTC(),
			},
		},
		{
			name: "http request",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					FieldRequest: httptest.NewRequest("GET", "/", nil),
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
				Request: &sentry.Request{
					URL:     "http://example.com/",
					Method:  http.MethodGet,
					Headers: map[string]string{"Host": "example.com"},
				},
			},
		},
		{
			name: "error",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					logrus.ErrorKey: errors.New("things failed"),
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
				Exception: []sentry.Exception{
					{Type: "error", Value: "things failed"},
				},
			},
		},
		{
			name: "non-error",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					logrus.ErrorKey: "this isn't really an error",
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{
					"error": "this isn't really an error",
				},
			},
		},
		{
			name: "error with stack trace",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					logrus.ErrorKey: pkgerr.WithStack(errors.New("failure")),
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
				Exception: []sentry.Exception{
					{Type: "error", Value: "failure", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{}}},
				},
			},
		},
		{
			name: "user",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					FieldUser: sentry.User{
						ID: "bob",
					},
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
				User: sentry.User{
					ID: "bob",
				},
			},
		},
		{
			name: "user pointer",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					FieldUser: &sentry.User{
						ID: "alice",
					},
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{},
				User: sentry.User{
					ID: "alice",
				},
			},
		},
		{
			name: "non-user",
			entry: &logrus.Entry{
				Data: map[string]interface{}{
					FieldUser: "just say no to drugs",
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]interface{}{
					"user": "just say no to drugs",
				},
			},
		},
	}

	h, err := New(nil, sentry.ClientOptions{
		Dsn:              testDSN,
		AttachStacktrace: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := h.entryToEvent(tt.entry)
			opts := cmp.Options{
				cmpopts.IgnoreFields(sentry.Event{},
					"sdkMetaData",
				),
			}
			if d := cmp.Diff(tt.want, got, opts); d != "" {
				t.Error(d)
			}
		})
	}
}

func Test_exceptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		trace bool
		err   error
		want  []sentry.Exception
	}{
		{
			name:  "std error",
			trace: true,
			err:   errors.New("foo"),
			want: []sentry.Exception{
				{Type: "error", Value: "foo"},
			},
		},
		{
			name:  "wrapped, no stack",
			trace: true,
			err:   fmt.Errorf("foo: %w", errors.New("bar")),
			want: []sentry.Exception{
				{Type: "error", Value: "bar"},
				{Type: "error", Value: "foo: bar"},
			},
		},
		{
			name:  "ignored stack",
			trace: false,
			err:   pkgerr.New("foo"),
			want: []sentry.Exception{
				{Type: "error", Value: "foo"},
			},
		},
		{
			name:  "stack",
			trace: true,
			err:   pkgerr.New("foo"),
			want: []sentry.Exception{
				{Type: "error", Value: "foo", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{}}},
			},
		},
		{
			name:  "multi-wrapped error",
			trace: true,
			err: func() error {
				err := errors.New("original")
				err = fmt.Errorf("fmt: %w", err)
				err = pkgerr.Wrap(err, "wrap")
				err = pkgerr.WithStack(err)
				return fmt.Errorf("wrapped: %w", err)
			}(),
			want: []sentry.Exception{
				{Type: "error", Value: "original"},
				{Type: "error", Value: "fmt: original"},
				{Type: "error", Value: "wrap: fmt: original", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{}}},
				{Type: "error", Value: "wrap: fmt: original", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{}}},
				{Type: "error", Value: "wrapped: wrap: fmt: original"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := New(nil, sentry.ClientOptions{AttachStacktrace: tt.trace})
			if err != nil {
				t.Fatal(err)
			}
			got := h.exceptions(tt.err)

			if d := cmp.Diff(tt.want, got); d != "" {
				t.Error(d)
			}
		})
	}
}
