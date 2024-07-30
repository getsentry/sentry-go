//go:build go1.21

package sentryslog

import (
	"context"
	"encoding"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"

	"github.com/getsentry/sentry-go"
)

func source(sourceKey string, r *slog.Record) slog.Attr {
	fs := runtime.CallersFrames([]uintptr{r.PC})
	f, _ := fs.Next()
	var args []any
	if f.Function != "" {
		args = append(args, slog.String("function", f.Function))
	}
	if f.File != "" {
		args = append(args, slog.String("file", f.File))
	}
	if f.Line != 0 {
		args = append(args, slog.Int("line", f.Line))
	}

	return slog.Group(sourceKey, args...)
}

type replaceAttrFn = func(groups []string, a slog.Attr) slog.Attr

func replaceAttrs(fn replaceAttrFn, groups []string, attrs ...slog.Attr) []slog.Attr {
	for i := range attrs {
		attr := attrs[i]
		value := attr.Value.Resolve()
		if value.Kind() == slog.KindGroup {
			attrs[i].Value = slog.GroupValue(replaceAttrs(fn, append(groups, attr.Key), value.Group()...)...)
		} else if fn != nil {
			attrs[i] = fn(groups, attr)
		}
	}

	return attrs
}

func extractError(attrs []slog.Attr) ([]slog.Attr, error) {
	for i := range attrs {
		attr := attrs[i]

		if _, ok := errorKeys[attr.Key]; !ok {
			continue
		}

		if err, ok := attr.Value.Resolve().Any().(error); ok {
			return append(attrs[:i], attrs[i+1:]...), err
		}
	}

	return attrs, nil
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
	} else if attr.Key == "transaction" && kind == slog.KindGroup {
		event.Transaction = v.String()
	} else if attr.Key == "user" && kind == slog.KindGroup {
		data := attrsToString(v.Group()...)

		if id, ok := data["id"]; ok {
			event.User.ID = id
			delete(data, "id")
		} else if email, ok := data["email"]; ok {
			event.User.Email = email
			delete(data, "email")
		} else if ipAddress, ok := data["ip_address"]; ok {
			event.User.IPAddress = ipAddress
			delete(data, "ip_address")
		} else if username, ok := data["username"]; ok {
			event.User.Username = username
			delete(data, "username")
		} else if name, ok := data["name"]; ok {
			event.User.Name = name
			delete(data, "name")
		} else if segment, ok := data["segment"]; ok {
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
		event.Contexts[attr.Key] = attrsToMap(attr.Value.Group()...)
	} else {
		// "context" should not be added to underlying context layers (see slog.KindGroup case).
		if _, ok := event.Contexts[contextKey]; !ok {
			event.Contexts[contextKey] = make(map[string]any, 0)
		}
		event.Contexts[contextKey][attr.Key] = attr.Value.Any()
	}
}

func attrsToMap(attrs ...slog.Attr) map[string]any {
	output := make(map[string]any, len(attrs))

	attrsByKey := groupValuesByKey(attrs)
	for k, values := range attrsByKey {
		v := mergeAttrValues(values...)
		if v.Kind() == slog.KindGroup {
			output[k] = attrsToMap(v.Group()...)
		} else {
			output[k] = v.Any()
		}
	}

	return output
}

func mergeAttrValues(values ...slog.Value) slog.Value {
	v := values[0]

	for i := 1; i < len(values); i++ {
		if v.Kind() != slog.KindGroup || values[i].Kind() != slog.KindGroup {
			v = values[i]
			continue
		}

		v = slog.GroupValue(append(v.Group(), values[i].Group()...)...)
	}

	return v
}

func groupValuesByKey(attrs []slog.Attr) map[string][]slog.Value {
	result := map[string][]slog.Value{}

	for _, item := range attrs {
		key := item.Key
		result[key] = append(result[key], item.Value)
	}

	return result
}

func attrsToString(attrs ...slog.Attr) map[string]string {
	output := make(map[string]string, len(attrs))

	for _, attr := range attrs {
		k, v := attr.Key, attr.Value
		output[k] = valueToString(v)
	}

	return output
}

func valueToString(v slog.Value) string {
	switch v.Kind() {
	case slog.KindAny, slog.KindLogValuer, slog.KindGroup:
		return anyValueToString(v)
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", v.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%f", v.Float64())
	case slog.KindString:
		return v.String()
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().String()
	}
	return anyValueToString(v)
}

func anyValueToString(v slog.Value) string {
	tm, ok := v.Any().(encoding.TextMarshaler)
	if !ok {
		return fmt.Sprintf("%+v", v.Any())
	}

	data, err := tm.MarshalText()
	if err != nil {
		return fmt.Sprintf("%+v", v.Any())
	}

	return string(data)

}

func appendRecordAttrsToAttrs(attrs []slog.Attr, groups []string, record *slog.Record) []slog.Attr {
	output := make([]slog.Attr, len(attrs))
	copy(output, attrs)

	for i, j := 0, len(groups)-1; i < j; i, j = i+1, j-1 {
		groups[i], groups[j] = groups[j], groups[i]
	}
	record.Attrs(func(attr slog.Attr) bool {
		for i := range groups {
			attr = slog.Group(groups[i], attr)
		}
		output = append(output, attr)
		return true
	})

	return output
}

func removeEmptyAttrs(attrs []slog.Attr) []slog.Attr {
	result := []slog.Attr{}

	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}

		if attr.Value.Kind() == slog.KindGroup {
			values := removeEmptyAttrs(attr.Value.Group())
			if len(values) == 0 {
				continue
			}
			attr.Value = slog.GroupValue(values...)
			result = append(result, attr)
		} else if !attr.Value.Equal(slog.Value{}) {
			result = append(result, attr)
		}
	}

	return result
}

func contextExtractor(ctx context.Context, fns []func(ctx context.Context) []slog.Attr) []slog.Attr {
	attrs := []slog.Attr{}
	for _, fn := range fns {
		attrs = append(attrs, fn(ctx)...)
	}
	return attrs
}

func appendAttrsToGroup(groups []string, actualAttrs []slog.Attr, newAttrs ...slog.Attr) []slog.Attr {
	actualAttrsCopy := make([]slog.Attr, len(actualAttrs))
	copy(actualAttrsCopy, actualAttrs)

	if len(groups) == 0 {
		return uniqAttrs(append(actualAttrsCopy, newAttrs...))
	}

	groupKey := groups[0]
	for i := range actualAttrsCopy {
		attr := actualAttrsCopy[i]
		if attr.Key == groupKey && attr.Value.Kind() == slog.KindGroup {
			actualAttrsCopy[i] = slog.Group(groupKey, toAnySlice(appendAttrsToGroup(groups[1:], attr.Value.Group(), newAttrs...))...)
			return actualAttrsCopy
		}
	}

	return uniqAttrs(
		append(
			actualAttrsCopy,
			slog.Group(
				groupKey,
				toAnySlice(appendAttrsToGroup(groups[1:], []slog.Attr{}, newAttrs...))...,
			),
		),
	)
}

func toAnySlice(collection []slog.Attr) []any {
	result := make([]any, len(collection))
	for i := range collection {
		result[i] = collection[i]
	}
	return result
}

func uniqAttrs(attrs []slog.Attr) []slog.Attr {
	return uniqByLast(attrs, func(item slog.Attr) string {
		return item.Key
	})
}

func uniqByLast[T any, U comparable](collection []T, iteratee func(item T) U) []T {
	result := make([]T, 0, len(collection))
	seen := make(map[U]int, len(collection))
	seenIndex := 0

	for _, item := range collection {
		key := iteratee(item)

		if index, ok := seen[key]; ok {
			result[index] = item
			continue
		}

		seen[key] = seenIndex
		seenIndex++
		result = append(result, item)
	}

	return result
}
