package sentryzap

import (
	"fmt"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap/zapcore"
)

func setDefaultConfig(cfg *Configuration) {
	if cfg.MaxBreadcrumbs <= 0 {
		cfg.MaxBreadcrumbs = defaultMaxBreadcrumbs
	}
	if cfg.FlushTimeout <= 0 {
		cfg.FlushTimeout = 3 * time.Second
	}
	if cfg.FrameMatcher == nil {
		cfg.FrameMatcher = defaultFrameMatchers
	}
}

func unwrapError(err error) error {
	switch t := err.(type) {
	case interface{ Unwrap() error }:
		return t.Unwrap()
	case interface{ Cause() error }:
		return t.Cause()
	default:
		return nil
	}
}

func getTypeName(err error) string {
	if t, ok := err.(interface{ TypeName() string }); ok {
		return t.TypeName()
	}
	return reflect.TypeOf(err).String()
}

func getTypeOf(err error) string {
	return fmt.Sprintf("%s:%s", err.Error(), reflect.TypeOf(err).String())
}

func getScope(field zapcore.Field) *sentry.Scope {
	if field.Type == zapcore.SkipType {
		if scope, ok := field.Interface.(*sentry.Scope); ok && field.Key == zapSentryScopeKey {
			return scope
		}
	}
	return nil
}

func (c *core) filterFrames(frames []sentry.Frame) []sentry.Frame {
	filtered := frames[:0]
	for _, frame := range frames {
		if !c.cfg.FrameMatcher.Matches(frame) {
			filtered = append(filtered, frame)
		}
	}
	return filtered
}
