package sentryzap

import (
	"slices"
	"time"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type core struct {
	client *sentry.Client
	cfg    *Configuration
	zapcore.LevelEnabler
	flushTimeout time.Duration

	sentryScope *sentry.Scope
	errs        []error
	fields      map[string]any
}

func (c *core) With(fs []zapcore.Field) zapcore.Core {
	return c.with(fs)
}

func (c *core) with(fs []zapcore.Field) *core {
	fields := make(map[string]interface{}, len(c.fields)+len(fs))
	for k, v := range c.fields {
		fields[k] = v
	}

	errs := append([]error{}, c.errs...)
	var sentryScope *sentry.Scope

	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fs {
		f.AddTo(enc)
		switch f.Type {
		case zapcore.ErrorType:
			errs = append(errs, f.Interface.(error))
		case zapcore.SkipType:
			if scope := getScope(f); scope != nil {
				sentryScope = scope
			}
		}
	}

	for k, v := range enc.Fields {
		fields[k] = v
	}

	return &core{
		client:       c.client,
		cfg:          c.cfg,
		LevelEnabler: c.LevelEnabler,
		flushTimeout: c.flushTimeout,
		sentryScope:  sentryScope,
		errs:         errs,
		fields:       fields,
	}
}

func (c *core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.cfg.EnableBreadcrumbs && c.cfg.BreadcrumbLevel.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	if c.cfg.Level.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *core) Write(ent zapcore.Entry, fs []zapcore.Field) error {
	clone := c.with(c.addSpecialFields(ent, fs))

	if c.shouldAddBreadcrumb(ent.Level) {
		c.addBreadcrumb(ent, clone.fields)
	}

	if c.shouldLogEvent(ent.Level) {
		c.logEvent(ent, fs, clone)
	}

	if ent.Level > zapcore.ErrorLevel {
		return c.Sync()
	}
	return nil
}

func (c *core) Sync() error {
	c.client.Flush(c.flushTimeout)
	return nil
}

func (c *core) shouldAddBreadcrumb(level zapcore.Level) bool {
	return c.cfg.EnableBreadcrumbs && c.cfg.BreadcrumbLevel.Enabled(level)
}

func (c *core) shouldLogEvent(level zapcore.Level) bool {
	return c.cfg.Level.Enabled(level)
}

func (c *core) addBreadcrumb(ent zapcore.Entry, fields map[string]interface{}) {
	breadcrumb := sentry.Breadcrumb{
		Message:   ent.Message,
		Data:      fields,
		Level:     levelMap[ent.Level],
		Timestamp: ent.Time,
	}
	c.scope().AddBreadcrumb(&breadcrumb, c.cfg.MaxBreadcrumbs)
}

func (c *core) logEvent(ent zapcore.Entry, fs []zapcore.Field, clone *core) {
	event := sentry.NewEvent()
	event.Message = ent.Message
	event.Timestamp = ent.Time
	event.Level = levelMap[ent.Level]
	event.Tags = c.collectTags(fs)
	event.Extra = clone.fields
	event.Exception = clone.createExceptions()

	if event.Exception == nil && !c.cfg.DisableStacktrace && c.client.Options().AttachStacktrace {
		stacktrace := sentry.NewStacktrace()
		if stacktrace != nil {
			stacktrace.Frames = c.filterFrames(stacktrace.Frames)
			event.Threads = []sentry.Thread{{Stacktrace: stacktrace, Current: true}}
		}
	}

	hint := c.getEventHint(fs)
	c.client.CaptureEvent(event, hint, c.scope())
}

func (c *core) addSpecialFields(ent zapcore.Entry, fs []zapcore.Field) []zapcore.Field {
	if c.cfg.LoggerNameKey != "" && ent.LoggerName != "" {
		fs = append(fs, zap.String(c.cfg.LoggerNameKey, ent.LoggerName))
	}
	return fs
}

func (c *core) createExceptions() []sentry.Exception {
	if len(c.errs) == 0 {
		return nil
	}

	processedErrors := make(map[string]struct{})
	exceptions := []sentry.Exception{}

	for i := len(c.errs) - 1; i >= 0; i-- {
		exceptions = c.addExceptionsFromError(exceptions, processedErrors, c.errs[i])
	}

	slices.Reverse(exceptions)
	return exceptions
}

func (c *core) collectTags(fs []zapcore.Field) map[string]string {
	tags := make(map[string]string, len(c.cfg.Tags))
	for k, v := range c.cfg.Tags {
		tags[k] = v
	}
	for _, f := range fs {
		if f.Type == zapcore.SkipType {
			if tag, ok := f.Interface.(tagField); ok {
				tags[tag.Key] = tag.Value
			}
		}
	}
	return tags
}

func (c *core) addExceptionsFromError(
	exceptions []sentry.Exception,
	processedErrors map[string]struct{},
	err error,
) []sentry.Exception {
	for i := 0; i < maxErrorDepth && err != nil; i++ {
		key := getTypeOf(err)
		if _, seen := processedErrors[key]; seen {
			break
		}
		processedErrors[key] = struct{}{}

		exception := sentry.Exception{Value: err.Error(), Type: getTypeName(err)}
		if !c.cfg.DisableStacktrace {
			stacktrace := sentry.ExtractStacktrace(err)
			if stacktrace != nil {
				stacktrace.Frames = c.filterFrames(stacktrace.Frames)
				exception.Stacktrace = stacktrace
			}
		}
		exceptions = append(exceptions, exception)

		err = unwrapError(err)
	}
	return exceptions
}

func (c *core) getEventHint(fs []zapcore.Field) *sentry.EventHint {
	for _, f := range fs {
		if f.Type == zapcore.SkipType {
			if ctxField, ok := f.Interface.(ctxField); ok {
				return &sentry.EventHint{Context: ctxField.Value}
			}
		}
	}
	return nil
}

func (c *core) hub() *sentry.Hub {
	if c.cfg.Hub != nil {
		return c.cfg.Hub
	}

	return sentry.CurrentHub()
}

func (c *core) scope() *sentry.Scope {
	if c.sentryScope != nil {
		return c.sentryScope
	}

	return c.hub().Scope()
}
