package sentryzap

import (
	"strings"

	"github.com/getsentry/sentry-go"
)

type (
	FrameMatchers                  []FrameMatcher
	FrameMatcherFunc               func(f sentry.Frame) bool
	SkipModulePrefixFrameMatcher   string
	SkipFunctionPrefixFrameMatcher string
)

type FrameMatcher interface {
	Matches(f sentry.Frame) bool
}

var (
	defaultFrameMatchers = FrameMatchers{
		SkipModulePrefixFrameMatcher("go.uber.org/zap"),
	}
)

func (f FrameMatcherFunc) Matches(frame sentry.Frame) bool {
	return f(frame)
}

func (f SkipModulePrefixFrameMatcher) Matches(frame sentry.Frame) bool {
	return strings.HasPrefix(frame.Module, string(f))
}

func (f SkipFunctionPrefixFrameMatcher) Matches(frame sentry.Frame) bool {
	return strings.HasPrefix(frame.Function, string(f))
}

func (ff FrameMatchers) Matches(frame sentry.Frame) bool {
	for i := range ff {
		if ff[i].Matches(frame) {
			return true
		}
	}
	return false
}

func CombineFrameMatchers(matcher ...FrameMatcher) FrameMatcher {
	return FrameMatchers(matcher)
}
