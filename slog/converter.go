//go:build go1.21

package sentryslog

import (
	"encoding"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getsentry/sentry-go"
)

var (
	sourceKey = "source"
	errorKeys = map[string]struct{}{
		"error": {},
		"err":   {},
	}
	name = "slog"
)

type Converter func(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record, hub *sentry.Hub) *sentry.Event

func DefaultConverter(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record, _ *sentry.Hub) *sentry.Event {
	// aggregate all attributes
	attrs := appendRecordAttrsToAttrs(loggerAttr, groups, record)

	// developer formatters
	if addSource {
		attrs = append(attrs, source(sourceKey, record))
	}
	attrs = replaceAttrs(replaceAttr, []string{}, attrs...)
	attrs = removeEmptyAttrs(attrs)
	attrs, err := extractError(attrs)

	// handler formatter
	event := sentry.NewEvent()
	event.Timestamp = record.Time.UTC()
	event.Level = LogLevels[record.Level]
	event.Message = record.Message
	event.Logger = name
	event.SetException(err, 10)

	for i := range attrs {
		attrToSentryEvent(attrs[i], event)
	}

	return event
}

func attrToSentryEvent(attr slog.Attr, event *sentry.Event) {
	k := attr.Key
	v := attr.Value
	kind := attr.Value.Kind()

	if k == "dist" && kind == slog.KindString {
		event.Dist = v.String()
	} else if k == "environment" && kind == slog.KindString {
		event.Environment = v.String()
	} else if k == "event_id" && kind == slog.KindString {
		event.EventID = sentry.EventID(v.String())
	} else if k == "platform" && kind == slog.KindString {
		event.Platform = v.String()
	} else if k == "release" && kind == slog.KindString {
		event.Release = v.String()
	} else if k == "server_name" && kind == slog.KindString {
		event.ServerName = v.String()
	} else if attr.Key == "tags" && kind == slog.KindGroup {
		event.Tags = attrsToString(v.Group()...)
	} else if attr.Key == "transaction" && kind == slog.KindString {
		// The implementation used slog.KindGroup instead of slog.KindString
		event.Transaction = v.String()
	} else if attr.Key == "user" && kind == slog.KindGroup {
		data := attrsToString(v.Group()...)
		if id, ok := data["id"]; ok {
			event.User.ID = id
			delete(data, "id")
		}
		if email, ok := data["email"]; ok {
			event.User.Email = email
			delete(data, "email")
		}
		if ipAddress, ok := data["ip_address"]; ok {
			event.User.IPAddress = ipAddress
			delete(data, "ip_address")
		}
		if username, ok := data["username"]; ok {
			event.User.Username = username
			delete(data, "username")
		}
		if name, ok := data["name"]; ok {
			event.User.Name = name
			delete(data, "name")
		}
		if segment, ok := data["segment"]; ok {
			event.User.Segment = segment
			delete(data, "segment")
		}

		event.User.Data = data
	} else if attr.Key == "request" && kind == slog.KindAny {
		if req, ok := attr.Value.Any().(http.Request); ok {
			event.Request = sentry.NewRequest(&req)
		} else if req, ok := attr.Value.Any().(*http.Request); ok {
			event.Request = sentry.NewRequest(req)
		} else {
			if tm, ok := v.Any().(encoding.TextMarshaler); ok {
				data, err := tm.MarshalText()
				if err == nil {
					event.User.Data["request"] = string(data)
				} else {
					event.User.Data["request"] = fmt.Sprintf("%v", v.Any())
				}
			}
		}
	} else if kind == slog.KindGroup {
		event.Extra[attr.Key] = attrsToMap(attr.Value.Group()...)
	} else {
		event.Extra[attr.Key] = attr.Value.Any()
	}
}
