package sentryslog

import (
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConverter(t *testing.T) {
	// Mock data
	mockAttr := slog.Attr{
		Key:   "mockKey",
		Value: slog.StringValue("mockValue"),
	}
	mockLoggerAttr := []slog.Attr{mockAttr}
	mockGroups := []string{"group1", "group2"}
	mockRecord := &slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "Test message",
	}

	// Mock replaceAttr function
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		return a
	}

	// Call DefaultConverter function
	event := DefaultConverter(true, replaceAttr, mockLoggerAttr, mockGroups, mockRecord, nil)

	// Assertions
	assert.NotNil(t, event)
	assert.Equal(t, mockRecord.Time.UTC(), event.Timestamp)
	assert.Equal(t, LogLevels[mockRecord.Level], event.Level)
	assert.Equal(t, mockRecord.Message, event.Message)
	assert.Equal(t, name, event.Logger)

	// Check if the attributes are correctly converted
	var foundMockKey bool
	for key, value := range event.Extra {
		if key == "mockKey" && value == "mockValue" {
			foundMockKey = true
			break
		}
	}
	assert.True(t, foundMockKey)
}

func TestAttrToSentryEvent(t *testing.T) {
	reqURL, _ := url.Parse("http://example.com")

	tests := map[string]struct {
		attr     slog.Attr
		expected *sentry.Event
	}{
		"dist": {
			attr:     slog.Attr{Key: "dist", Value: slog.StringValue("dist_value")},
			expected: &sentry.Event{Dist: "dist_value"},
		},
		"environment": {
			attr:     slog.Attr{Key: "environment", Value: slog.StringValue("env_value")},
			expected: &sentry.Event{Environment: "env_value"},
		},
		"event_id": {
			attr:     slog.Attr{Key: "event_id", Value: slog.StringValue("event_id_value")},
			expected: &sentry.Event{EventID: sentry.EventID("event_id_value")},
		},
		"platform": {
			attr:     slog.Attr{Key: "platform", Value: slog.StringValue("platform_value")},
			expected: &sentry.Event{Platform: "platform_value"},
		},
		"release": {
			attr:     slog.Attr{Key: "release", Value: slog.StringValue("release_value")},
			expected: &sentry.Event{Release: "release_value"},
		},
		"server_name": {
			attr:     slog.Attr{Key: "server_name", Value: slog.StringValue("server_name_value")},
			expected: &sentry.Event{ServerName: "server_name_value"},
		},
		"tags": {
			attr: slog.Attr{Key: "tags", Value: slog.GroupValue(
				slog.Attr{Key: "tag1", Value: slog.StringValue("value1")},
				slog.Attr{Key: "tag2", Value: slog.StringValue("value2")},
			)},
			expected: &sentry.Event{Tags: map[string]string{"tag1": "value1", "tag2": "value2"}},
		},
		"transaction": {
			attr:     slog.Attr{Key: "transaction", Value: slog.StringValue("transaction_value")},
			expected: &sentry.Event{Transaction: "transaction_value"},
		},
		"user": {
			attr: slog.Attr{Key: "user", Value: slog.GroupValue(
				slog.Attr{Key: "id", Value: slog.StringValue("user_id")},
				slog.Attr{Key: "email", Value: slog.StringValue("user_email")},
				slog.Attr{Key: "ip_address", Value: slog.StringValue("user_ip_address")},
				slog.Attr{Key: "username", Value: slog.StringValue("user_username")},
				slog.Attr{Key: "name", Value: slog.StringValue("user_name")},
			)},
			expected: &sentry.Event{
				User: sentry.User{
					ID:        "user_id",
					Email:     "user_email",
					IPAddress: "user_ip_address",
					Username:  "user_username",
					Name:      "user_name",
					Data:      map[string]string{},
				},
			},
		},
		"request": {
			attr: slog.Attr{Key: "request", Value: slog.AnyValue(&http.Request{
				Method: "GET",
				URL:    reqURL,
				Header: http.Header{},
			})},
			expected: &sentry.Event{Request: &sentry.Request{
				Method: "GET",
				URL:    "http://",
				Headers: map[string]string{
					"Host": "",
				},
			}},
		},
		"request_ptr": {
			attr: slog.Attr{Key: "request", Value: slog.AnyValue(http.Request{
				Method: "GET",
				URL:    reqURL,
				Header: http.Header{},
			})},
			expected: &sentry.Event{Request: &sentry.Request{
				Method: "GET",
				URL:    "http://",
				Headers: map[string]string{
					"Host": "",
				},
			}},
		},
		"request_str": {
			attr:     slog.Attr{Key: "request", Value: slog.StringValue("GET http://")},
			expected: &sentry.Event{Extra: map[string]any{"request": "GET http://"}},
		},
		"context_group": {
			attr: slog.Attr{Key: "context", Value: slog.GroupValue(
				slog.Attr{Key: "key1", Value: slog.StringValue("value1")},
				slog.Attr{Key: "key2", Value: slog.StringValue("value2")},
			)},
			expected: &sentry.Event{Extra: map[string]any{
				"context": map[string]any{
					"key1": "value1",
					"key2": "value2",
				}},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			event := sentry.NewEvent()
			attrToSentryEvent(tc.attr, event)
			assert.Equal(t, tc.expected.Dist, event.Dist)
			assert.Equal(t, tc.expected.Environment, event.Environment)
			assert.Equal(t, tc.expected.EventID, event.EventID)
			assert.Equal(t, tc.expected.Platform, event.Platform)
			assert.Equal(t, tc.expected.Release, event.Release)
			assert.Equal(t, tc.expected.ServerName, event.ServerName)
			assert.Equal(t, tc.expected.Transaction, event.Transaction)
			assert.Equal(t, tc.expected.User, event.User)
			assert.Equal(t, tc.expected.Request, event.Request)
			if len(tc.expected.Tags) == 0 {
				assert.Empty(t, event.Tags)
			} else {
				assert.Equal(t, tc.expected.Tags, event.Tags)
			}
			if len(tc.expected.Extra) == 0 {
				assert.Empty(t, event.Extra)
			} else {
				assert.Equal(t, tc.expected.Extra, event.Extra)
			}
		})
	}
}
