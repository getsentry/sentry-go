package sentrylogrus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
)

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
		h, err := New(nil, sentry.ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if id := h.hubProvider().CaptureEvent(&sentry.Event{}); id == nil {
			t.Error("CaptureEvent failed")
		}
		if !h.hubProvider().Client().Flush(testutils.FlushTimeout()) {
			t.Error("flush failed")
		}
	})
}

func TestSetHubProvider(t *testing.T) {
	t.Parallel()

	h, err := New(nil, sentry.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Custom HubProvider to ensure separate hubs for each test
	h.SetHubProvider(func() *sentry.Hub {
		client, _ := sentry.NewClient(sentry.ClientOptions{})
		return sentry.NewHub(client, sentry.NewScope())
	})

	entry := &logrus.Entry{Level: logrus.ErrorLevel}
	if err := h.Fire(entry); err != nil {
		t.Fatal(err)
	}

	if !h.hubProvider().Client().Flush(testutils.FlushTimeout()) {
		t.Error("flush failed")
	}
}

func TestFire(t *testing.T) {
	t.Parallel()

	entry := &logrus.Entry{
		Level: logrus.ErrorLevel,
	}

	opts := sentry.ClientOptions{}
	hook, err := New([]logrus.Level{logrus.ErrorLevel}, opts)
	if err != nil {
		t.Fatal(err)
	}

	err = hook.Fire(entry)
	if err != nil {
		t.Fatal(err)
	}

	if !hook.hubProvider().Client().Flush(testutils.FlushTimeout()) {
		t.Error("flush failed")
	}
}

func Test_entryToEvent(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		entry *logrus.Entry
		want  *sentry.Event
	}{
		"empty entry": {
			entry: &logrus.Entry{},
			want: &sentry.Event{
				Level:  "fatal",
				Extra:  map[string]any{},
				Logger: "logrus",
			},
		},
		"data fields": {
			entry: &logrus.Entry{
				Data: map[string]any{
					"foo": 123.4,
					"bar": "oink",
				},
			},
			want: &sentry.Event{
				Level:  "fatal",
				Extra:  map[string]any{"bar": "oink", "foo": 123.4},
				Logger: "logrus",
			},
		},
		"info level": {
			entry: &logrus.Entry{
				Level: logrus.InfoLevel,
			},
			want: &sentry.Event{
				Level:  "info",
				Extra:  map[string]any{},
				Logger: "logrus",
			},
		},
		"message": {
			entry: &logrus.Entry{
				Message: "the only thing we have to fear is fear itself",
			},
			want: &sentry.Event{
				Level:   "fatal",
				Extra:   map[string]any{},
				Message: "the only thing we have to fear is fear itself",
				Logger:  "logrus",
			},
		},
		"timestamp": {
			entry: &logrus.Entry{
				Time: time.Unix(1, 2).UTC(),
			},
			want: &sentry.Event{
				Level:     "fatal",
				Extra:     map[string]any{},
				Timestamp: time.Unix(1, 2).UTC(),
				Logger:    "logrus",
			},
		},
		"http request": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldRequest: httptest.NewRequest("GET", "/", nil),
				},
			},
			want: &sentry.Event{
				Level: "fatal",
				Extra: map[string]any{},
				Request: &sentry.Request{
					URL:     "http://example.com/",
					Method:  http.MethodGet,
					Headers: map[string]string{"Host": "example.com"},
				},
				Logger: "logrus",
			},
		},
	}

	h, err := New(nil, sentry.ClientOptions{
		AttachStacktrace: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Custom HubProvider for test environment
	h.SetHubProvider(func() *sentry.Hub {
		client, _ := sentry.NewClient(sentry.ClientOptions{})
		return sentry.NewHub(client, sentry.NewScope())
	})

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := h.entryToEvent(tt.entry)
			opts := cmp.Options{
				cmpopts.IgnoreFields(sentry.Event{}, "sdkMetaData"),
			}
			if d := cmp.Diff(tt.want, got, opts); d != "" {
				t.Error(d)
			}
		})
	}
}
