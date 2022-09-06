package logrusentry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	pkgerr "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	aleerr "gitlab.com/flimzy/ale/errors"
	"gitlab.com/flimzy/testy"
)

const testDSN = "http://test:test@localhost/1234"

func xport(req *http.Request) http.RoundTripper {
	return testy.HTTPResponder(func(r *http.Request) (*http.Response, error) {
		*req = *r
		return &http.Response{}, nil
	})
}

func TestNew(t *testing.T) {
	t.Parallel()
	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()
		_, err := New(nil, ClientOptions{Dsn: "%xxx"})
		testy.Error(t, `[Sentry] DsnParseError: invalid url: parse "%xxx": invalid URL escape "%xx"`, err)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		req := new(http.Request)
		h, err := New(nil, ClientOptions{
			Dsn:           testDSN,
			HTTPTransport: xport(req),
		})
		if err != nil {
			t.Fatal(err)
		}
		if id := h.client.CaptureEvent(&sentry.Event{}, nil, nil); id == nil {
			t.Error("CaptureEvent failed")
		}
		if !h.Flush(5 * time.Second) {
			t.Error("flush failed")
		}
		testEvent(t, req.Body)
	})
}

func TestFire(t *testing.T) {
	t.Parallel()
	type tt struct {
		opts   ClientOptions
		levels []logrus.Level
		entry  *logrus.Entry
		err    string
	}

	tests := testy.NewTable()
	tests.Add("error", tt{
		levels: []logrus.Level{logrus.ErrorLevel},
		entry: &logrus.Entry{
			Level: logrus.ErrorLevel,
		},
	})

	tests.Run(t, func(t *testing.T, tt tt) {
		t.Parallel()
		req := new(http.Request)
		opts := tt.opts
		opts.Dsn = testDSN
		opts.HTTPTransport = xport(req)
		hook, err := New(tt.levels, opts)
		if err != nil {
			t.Fatal(err)
		}
		err = hook.Fire(tt.entry)
		testy.Error(t, tt.err, err)

		if !hook.Flush(5 * time.Second) {
			t.Error("flush failed")
		}
		testEvent(t, req.Body)
	})
}

func Test_e2e(t *testing.T) {
	t.Parallel()
	type tt struct {
		levels  []logrus.Level
		opts    ClientOptions
		init    func(*Hook)
		log     func(*logrus.Logger)
		skipped bool
	}

	tests := testy.NewTable()
	tests.Add("skip info", tt{
		levels: []logrus.Level{logrus.ErrorLevel},
		log: func(l *logrus.Logger) {
			l.Info("foo")
		},
		skipped: true,
	})
	tests.Add("error level", tt{
		levels: []logrus.Level{logrus.ErrorLevel},
		log: func(l *logrus.Logger) {
			l.Error("foo")
		},
	})
	tests.Add("metadata", tt{
		levels: []logrus.Level{logrus.ErrorLevel},
		opts: ClientOptions{
			Environment: "production",
			ServerName:  "localhost",
			Release:     "v1.2.3",
			Dist:        "beta",
		},
		log: func(l *logrus.Logger) {
			l.Error("foo")
		},
	})
	tests.Add("tags", tt{
		levels: []logrus.Level{logrus.ErrorLevel},
		opts: ClientOptions{
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
	})

	tests.Run(t, func(t *testing.T, tt tt) {
		t.Parallel()
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
		l.SetOutput(ioutil.Discard)
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
		testEvent(t, req.Body)
	})
}

func testEvent(t *testing.T, r io.ReadCloser) {
	t.Helper()
	t.Cleanup(func() {
		_ = r.Close()
	})
	var event map[string]interface{}
	if err := json.NewDecoder(r).Decode(&event); err != nil {
		t.Fatal(err)
	}
	// normalize fields
	event["timestamp"] = "xxx"
	event["event_id"] = "yyy"
	event["contexts"] = "zzz"
	event["server_name"] = "server"
	if d := testy.DiffAsJSON(testy.Snapshot(t), event); d != nil {
		t.Error(d)
	}
}

func Test_entry2event(t *testing.T) {
	t.Parallel()
	tests := testy.NewTable()
	tests.Add("empty event", &logrus.Entry{})
	tests.Add("data fields", &logrus.Entry{
		Data: map[string]interface{}{
			"foo": 123.4,
			"bar": "oink",
		},
	})
	tests.Add("info level", &logrus.Entry{
		Level: logrus.InfoLevel,
	})
	tests.Add("message", &logrus.Entry{
		Message: "the only thing we have to fear is fear itself",
	})
	tests.Add("timestamp", &logrus.Entry{
		Time: time.Unix(1, 2).UTC(),
	})
	tests.Add("http request", &logrus.Entry{
		Data: map[string]interface{}{
			FieldRequest: httptest.NewRequest("GET", "/", nil),
		},
	})
	tests.Add("non-http request", &logrus.Entry{
		Data: map[string]interface{}{
			FieldRequest: "some other request type",
		},
	})
	tests.Add("error", &logrus.Entry{
		Data: map[string]interface{}{
			logrus.ErrorKey: errors.New("things failed"),
		},
	})
	tests.Add("non-error", &logrus.Entry{
		Data: map[string]interface{}{
			logrus.ErrorKey: "this isn't really an error",
		},
	})
	tests.Add("stack trace error", &logrus.Entry{
		Data: map[string]interface{}{
			logrus.ErrorKey: pkgerr.WithStack(errors.New("failure")),
		},
	})
	tests.Add("user", &logrus.Entry{
		Data: map[string]interface{}{
			FieldUser: User{
				ID: "bob",
			},
		},
	})
	tests.Add("user pointer", &logrus.Entry{
		Data: map[string]interface{}{
			FieldUser: &User{
				ID: "alice",
			},
		},
	})
	tests.Add("non-user", &logrus.Entry{
		Data: map[string]interface{}{
			FieldUser: "just say no to drugs",
		},
	})

	res := []testy.Replacement{
		{
			Regexp:      regexp.MustCompile(`\(len=\d+\) "[^"]+/backend/log/`),
			Replacement: `(len=XX) ".../backend/log/`,
		},
	}

	h, err := New(nil, ClientOptions{
		Dsn:              testDSN,
		AttachStacktrace: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests.Run(t, func(t *testing.T, tt *logrus.Entry) {
		t.Parallel()
		got := h.entry2event(tt)
		if d := testy.DiffInterface(testy.Snapshot(t), got, res...); d != nil {
			t.Error(d)
		}
	})
}

func Test_exceptions(t *testing.T) {
	t.Parallel()
	type tt struct {
		trace bool
		err   error
	}

	tests := testy.NewTable()
	tests.Add("std error", tt{
		trace: true,
		err:   errors.New("foo"),
	})
	tests.Add("wrapped, no stack", tt{
		trace: true,
		err:   fmt.Errorf("foo: %w", errors.New("bar")),
	})
	tests.Add("ignored stack", tt{
		trace: false,
		err:   pkgerr.New("foo"),
	})
	tests.Add("stack", tt{
		trace: true,
		err:   pkgerr.New("foo"),
	})
	tests.Add("emulate middleware", func() interface{} {
		err := errors.New("original")
		err = fmt.Errorf("fmt: %w", err)
		err = pkgerr.Wrap(err, "wrap")
		err = pkgerr.WithStack(err)
		err = aleerr.NewNotes().NoStack().Fields(aleerr.Fields{
			"field": "value",
		}).Wrap(err)
		return tt{
			trace: true,
			err:   err,
		}
	})
	tests.Add("failing", func() interface{} {
		err := errors.New("converting NULL to string is unsupported")
		err = fmt.Errorf("sql: Scan error on column index 7, name \"email\": %w", err)
		err = pkgerr.WithStack(err)
		err = aleerr.NewNotes().NoStack().Fields(aleerr.Fields{
			"field": "value",
		}).Wrap(err)
		return tt{
			trace: true,
			err:   err,
		}
	})

	tests.Run(t, func(t *testing.T, tt tt) {
		t.Parallel()
		h, err := New(nil, ClientOptions{AttachStacktrace: tt.trace})
		if err != nil {
			t.Fatal(err)
		}
		got := h.exceptions(tt.err)
		res := []testy.Replacement{
			{
				Regexp:      regexp.MustCompile(`AbsPath: \(string\) \(len=\d+\) ".*/backend/log`),
				Replacement: `AbsPath: (string) (len=XX) ".../backend/log`,
			},
			{
				Regexp:      regexp.MustCompile(`AbsPath: \(string\) \(len=\d+\) ".*/go/pkg/mod`),
				Replacement: `AbsPath: (string) (len=XX) ".../go/pkg/mod`,
			},
		}
		if d := testy.DiffInterface(testy.Snapshot(t), got, res...); d != nil {
			t.Error(d)
		}
	})
}
