package sentryzap_test

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func mockSentryClient(f func(event *sentry.Event)) *sentry.Client {
	client, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "",
		Transport: &transport{MockSendEvent: f},
	})
	return client
}

type transport struct {
	MockSendEvent func(event *sentry.Event)
}

// Flush waits until any buffered events are sent to the Sentry server, blocking
// for at most the given timeout. It returns false if the timeout was reached.
func (f *transport) Flush(_ time.Duration) bool { return true }

// Configure is called by the Client itself, providing it it's own ClientOptions.
func (f *transport) Configure(_ sentry.ClientOptions) {}

// SendEvent assembles a new packet out of Event and sends it to remote server.
// We use this method to capture the event for testing
func (f *transport) SendEvent(event *sentry.Event) {
	f.MockSendEvent(event)
}

func (f *transport) Close() {}

func TestLevelEnabler(t *testing.T) {
	lvl := zap.NewAtomicLevelAt(zap.PanicLevel)
	core, recordedLogs := observer.New(lvl)
	logger := zap.New(core)

	var recordedSentryEvent *sentry.Event
	sentryClient := mockSentryClient(func(event *sentry.Event) {
		recordedSentryEvent = event
	})

	core, err := sentryzap.NewCore(
		sentryzap.Configuration{Level: lvl},
		sentryzap.NewSentryClientFromClient(sentryClient),
	)
	if err != nil {
		t.Fatal(err)
	}
	newLogger := sentryzap.AttachCoreToLogger(core, logger)

	newLogger.Error("foo")
	if recordedLogs.Len() > 0 || recordedSentryEvent != nil {
		t.Errorf("expected no logs before level change")
		t.Logf("logs=%v", recordedLogs.All())
		t.Logf("events=%v", recordedSentryEvent)
	}

	lvl.SetLevel(zap.ErrorLevel)
	newLogger.Error("bar")
	if recordedLogs.Len() != 1 || recordedSentryEvent == nil {
		t.Errorf("expected exactly one log after level change")
		t.Logf("logs=%v", recordedLogs.All())
		t.Logf("events=%v", recordedSentryEvent)
	}
}

func TestBreadcrumbLevelEnabler(t *testing.T) {
	corelvl := zap.NewAtomicLevelAt(zap.ErrorLevel)
	breadlvl := zap.NewAtomicLevelAt(zap.PanicLevel)

	_, err := sentryzap.NewCore(
		sentryzap.Configuration{Level: corelvl, BreadcrumbLevel: breadlvl, EnableBreadcrumbs: true},
		sentryzap.NewSentryClientFromClient(mockSentryClient(func(event *sentry.Event) {})),
	)
	if !errors.Is(err, sentryzap.ErrInvalidBreadcrumbLevel) {
		t.Errorf("expected ErrInvalidBreadcrumbLevel, got %v", err)
	}
}
