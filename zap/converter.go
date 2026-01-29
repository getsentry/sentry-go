package sentryzap

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap/zapcore"
)

var encoder = zapcore.NewJSONEncoder(zapcore.EncoderConfig{
	TimeKey:        "time",
	LevelKey:       "level",
	NameKey:        "logger",
	CallerKey:      "caller",
	MessageKey:     "msg",
	StacktraceKey:  "stacktrace",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeLevel:    zapcore.LowercaseLevelEncoder,
	EncodeTime:     zapcore.RFC3339TimeEncoder,
	EncodeDuration: zapcore.StringDurationEncoder,
	EncodeCaller:   zapcore.ShortCallerEncoder,
})

// encodeAndExtractValue uses the zapcore.JSONEncoder to serialize custom serializable object/array types.
func encodeAndExtractValue(addToEncoder func(zapcore.Encoder) error) (json.RawMessage, error) {
	if err := addToEncoder(encoder); err != nil {
		return nil, err
	}
	buf, err := encoder.EncodeEntry(zapcore.Entry{}, nil)
	if err != nil {
		return nil, err
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		return buf.Bytes(), nil
	}

	if val, ok := result["_"]; ok {
		return val, nil
	}
	return buf.Bytes(), nil
}

// stringifyObject uses zap's JSON encoder to safely stringify an object.
func stringifyObject(obj zapcore.ObjectMarshaler) (string, error) {
	val, err := encodeAndExtractValue(func(enc zapcore.Encoder) error {
		return enc.AddObject("_", obj)
	})
	if err != nil {
		return "", err
	}
	return string(val), nil
}

// stringifyArray uses zap's JSON encoder to safely stringify an array.
func stringifyArray(arr zapcore.ArrayMarshaler) (string, error) {
	val, err := encodeAndExtractValue(func(enc zapcore.Encoder) error {
		return enc.AddArray("_", arr)
	})
	if err != nil {
		return "", err
	}
	return string(val), nil
}

// zapFieldToLogEntry converts a zap Field to a sentry LogEntry attribute.
//
//nolint:gocyclo
func zapFieldToLogEntry(entry sentry.LogEntry, field zapcore.Field) sentry.LogEntry {
	key := field.Key

	switch field.Type {
	case zapcore.BoolType:
		return entry.Bool(key, field.Integer == 1)
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
		zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		return entry.Int64(key, field.Integer)
	case zapcore.Uint64Type:
		if uint64(field.Integer) <= math.MaxInt64 {
			return entry.Int64(key, field.Integer)
		}
		return entry.String(key, strconv.FormatUint(uint64(field.Integer), 10))
	case zapcore.UintptrType:
		return entry.String(key, fmt.Sprintf("0x%x", field.Integer))
	case zapcore.Float64Type:
		return entry.Float64(key, math.Float64frombits(uint64(field.Integer)))
	case zapcore.Float32Type:
		return entry.Float64(key, float64(math.Float32frombits(uint32(field.Integer))))
	case zapcore.StringType:
		return entry.String(key, field.String)
	case zapcore.ByteStringType, zapcore.BinaryType:
		return entry.String(key, string(field.Interface.([]byte)))
	case zapcore.StringerType:
		if stringer, ok := field.Interface.(fmt.Stringer); ok && stringer != nil {
			return entry.String(key, stringer.String())
		}
		return entry.String(key, fmt.Sprintf("%v", field.Interface))
	case zapcore.DurationType:
		duration := time.Duration(field.Integer)
		return entry.String(key, duration.String())
	case zapcore.TimeType:
		t := time.Unix(0, field.Integer)
		return entry.String(key, t.Format(time.RFC3339))
	case zapcore.TimeFullType:
		if t, ok := field.Interface.(time.Time); ok {
			return entry.String(key, t.Format(time.RFC3339))
		}
		return entry.String(key, fmt.Sprintf("%v", field.Interface))
	case zapcore.ErrorType:
		if err, ok := field.Interface.(error); ok && err != nil {
			return entry.String(key, err.Error())
		}
		return entry.String(key, fmt.Sprintf("%v", field.Interface))
	case zapcore.Complex128Type:
		if c, ok := field.Interface.(complex128); ok {
			return entry.String(key, strconv.FormatComplex(c, 'E', -1, 128))
		}
		return entry.String(key, fmt.Sprintf("%v", field.Interface))
	case zapcore.Complex64Type:
		if c, ok := field.Interface.(complex64); ok {
			return entry.String(key, strconv.FormatComplex(complex128(c), 'E', -1, 64))
		}
		return entry.String(key, fmt.Sprintf("%v", field.Interface))
	case zapcore.ReflectType:
		return entry.String(key, fmt.Sprintf("%+v", field.Interface))
	case zapcore.NamespaceType, zapcore.SkipType:
		// Namespace fields are just markers for grouping subsequent fields, so we skip them.
		return entry
	case zapcore.ObjectMarshalerType:
		if marshaler, ok := field.Interface.(zapcore.ObjectMarshaler); ok && marshaler != nil {
			if str, err := stringifyObject(marshaler); err == nil {
				return entry.String(key, str)
			}
		}
		return entry.String(key, fmt.Sprintf("%+v", field.Interface))
	case zapcore.ArrayMarshalerType:
		if marshaler, ok := field.Interface.(zapcore.ArrayMarshaler); ok && marshaler != nil {
			if str, err := stringifyArray(marshaler); err == nil {
				return entry.String(key, str)
			}
		}
		return entry.String(key, fmt.Sprintf("%+v", field.Interface))
	case zapcore.InlineMarshalerType:
		if marshaler, ok := field.Interface.(zapcore.ObjectMarshaler); ok && marshaler != nil {
			if str, err := stringifyObject(marshaler); err == nil {
				return entry.String(key, str)
			}
		}
		return entry.String(key, fmt.Sprintf("%+v", field.Interface))
	default:
		// Fallback for any unknown types
		if field.Interface != nil {
			return entry.String(key, fmt.Sprintf("%+v", field.Interface))
		}
		return entry.String(key, fmt.Sprintf("%d", field.Integer))
	}
}
