package sentryslog

import (
	"encoding"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
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
	kind := v.Kind()

	switch {
	case k == "dist" && kind == slog.KindString:
		event.Dist = v.String()
	case k == "environment" && kind == slog.KindString:
		event.Environment = v.String()
	case k == "event_id" && kind == slog.KindString:
		event.EventID = sentry.EventID(v.String())
	case k == "platform" && kind == slog.KindString:
		event.Platform = v.String()
	case k == "release" && kind == slog.KindString:
		event.Release = v.String()
	case k == "server_name" && kind == slog.KindString:
		event.ServerName = v.String()
	case k == "tags" && kind == slog.KindGroup:
		event.Tags = attrsToString(v.Group()...)
	case k == "transaction" && kind == slog.KindString:
		event.Transaction = v.String()
	case k == "user" && kind == slog.KindGroup:
		handleUserAttributes(v, event)
	case k == "request" && kind == slog.KindAny:
		handleRequestAttributes(v, event)
	case kind == slog.KindGroup:
		event.Extra[k] = attrsToMap(v.Group()...)
	default:
		event.Extra[k] = v.Any()
	}
}

func handleUserAttributes(v slog.Value, event *sentry.Event) {
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
	event.User.Data = data
}

func handleRequestAttributes(v slog.Value, event *sentry.Event) {
	if req, ok := v.Any().(http.Request); ok {
		event.Request = sentry.NewRequest(&req)
	} else if req, ok := v.Any().(*http.Request); ok {
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
}

func attrToSentryLog(group string, a slog.Attr) []attribute.Builder {
	switch a.Value.Kind() {
	case slog.KindAny:
		return []attribute.Builder{attribute.String(group+a.Key, fmt.Sprintf("%+v", a.Value.Any()))}
	case slog.KindBool:
		return []attribute.Builder{attribute.Bool(group+a.Key, a.Value.Bool())}
	case slog.KindDuration:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.Duration().String())}
	case slog.KindFloat64:
		return []attribute.Builder{attribute.Float64(group+a.Key, a.Value.Float64())}
	case slog.KindInt64:
		return []attribute.Builder{attribute.Int64(group+a.Key, a.Value.Int64())}
	case slog.KindString:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.String())}
	case slog.KindTime:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.Time().Format(time.RFC3339))}
	case slog.KindUint64:
		val := a.Value.Uint64()
		if val <= math.MaxInt64 {
			return []attribute.Builder{attribute.Int64(group+a.Key, int64(val))}
		} else {
			return []attribute.Builder{attribute.String(group+a.Key, strconv.FormatUint(val, 10))}
		}
	case slog.KindLogValuer:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.LogValuer().LogValue().String())}
	case slog.KindGroup:
		// Handle nested group attributes
		var attrs []attribute.Builder
		groupPrefix := group + a.Key
		if groupPrefix != "" {
			groupPrefix += "."
		}
		for _, subAttr := range a.Value.Group() {
			attrs = append(attrs, attrToSentryLog(groupPrefix, subAttr)...)
		}
		return attrs
	}

	sentry.DebugLogger.Printf("Invalid type: dropping attribute with key: %v and value: %v", a.Key, a.Value)
	return []attribute.Builder{}
}
