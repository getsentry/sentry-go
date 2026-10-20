// Package sentryasynq reports github.com/hibiken/asynq periodic tasks to
// Sentry Crons.
package sentryasynq

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/cronspec"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/hibiken/asynq"
)

const (
	slugHeader   = "sentry-monitor-slug"
	configHeader = "sentry-monitor-config"
)

// Options configures how periodic tasks are monitored.
type Options struct {
	// Location is the scheduler's time zone, as set in
	// asynq.SchedulerOpts.Location. Defaults to UTC, like asynq.
	Location *time.Location
	// MonitorConfig sets the monitor fields other than the schedule and
	// time zone, such as CheckInMargin and MaxRuntime. It may be nil.
	MonitorConfig *sentry.MonitorConfig
	// Exclude lists task types that are not monitored.
	Exclude []string
	// ExcludePatterns lists patterns of task types that are not monitored.
	ExcludePatterns []*regexp.Regexp
}

// NewConfigProvider returns a provider for asynq.PeriodicTaskManager that
// monitors every task of p, using the task type as the monitor slug.
// Each run then sends check-ins from workers that use Middleware.
//
// Tasks whose type is shared by other configs are not monitored, since
// they would report to the same monitor. Use WithMonitor to give them their
// own slugs.
func NewConfigProvider(p asynq.PeriodicTaskConfigProvider, options Options) asynq.PeriodicTaskConfigProvider {
	return configProvider{p, options}
}

type configProvider struct {
	asynq.PeriodicTaskConfigProvider
	options Options
}

func (p configProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	configs, err := p.PeriodicTaskConfigProvider.GetConfigs()
	counts := make(map[string]int)
	for _, c := range configs {
		if c != nil && c.Task != nil {
			counts[c.Task.Type()]++
		}
	}
	for i, c := range configs {
		if c == nil || c.Task == nil || c.Task.Headers()[slugHeader] != "" {
			continue
		}
		taskType := c.Task.Type()
		if p.options.excluded(taskType) {
			continue
		}
		if counts[taskType] > 1 {
			debuglog.Printf("sentryasynq: task type %q is used by more than one periodic task; use WithMonitor to monitor them", taskType)
			continue
		}
		monitored := *c
		monitored.Task = WithMonitor(c.Task, taskType, c.Cronspec, p.options)
		configs[i] = &monitored
	}
	return configs, err
}

func (o Options) excluded(taskType string) bool {
	if slices.Contains(o.Exclude, taskType) {
		return true
	}
	return slices.ContainsFunc(o.ExcludePatterns, func(re *regexp.Regexp) bool {
		return re.MatchString(taskType)
	})
}

// WithMonitor returns task marked to be reported as check-ins for the
// monitor slug when it is processed by a handler wrapped with Middleware.
// The monitor is created or updated with spec as its schedule when Sentry
// can represent it.
//
// Pass the returned task to asynq.Scheduler.Register with the same spec.
// Options passed to asynq.NewTask cannot be copied to a new task, so a task
// created with options and without headers is returned unchanged; pass the
// options to Register instead, or create the task with
// asynq.NewTaskWithHeaders.
func WithMonitor(task *asynq.Task, slug, spec string, options Options) *asynq.Task {
	loc := options.Location
	if loc == nil {
		loc = time.UTC
	}
	headers := map[string]string{slugHeader: slug}
	if config := cronspec.MonitorConfig(spec, loc, options.MonitorConfig); config != nil {
		if b, err := json.Marshal(config); err == nil {
			headers[configHeader] = string(b)
		}
	}
	if task.Headers() != nil {
		for k, v := range headers {
			task.Headers()[k] = v
		}
		return task
	}
	if hasOptions(task) {
		debuglog.Printf("sentryasynq: task %q has options, so it cannot be monitored; pass them to Register instead", task.Type())
		return task
	}
	return asynq.NewTaskWithHeaders(task.Type(), task.Payload(), headers)
}

// hasOptions reports whether options were passed to asynq.NewTask, which has
// no accessor for them.
func hasOptions(task *asynq.Task) bool {
	opts := reflect.ValueOf(task).Elem().FieldByName("opts")
	return !opts.IsValid() || opts.Len() > 0
}

// Middleware sends check-ins around tasks marked by NewConfigProvider or
// WithMonitor, and runs other tasks unchanged. The task's error is returned
// unchanged.
func Middleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		slug := task.Headers()[slugHeader]
		if slug == "" {
			return next.ProcessTask(ctx, task)
		}
		return sentry.WithMonitor(ctx, slug, decodeConfig(task.Headers()[configHeader]), func() error {
			return next.ProcessTask(ctx, task)
		})
	})
}

type wireConfig struct {
	Schedule *struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
		Unit  string          `json:"unit"`
	} `json:"schedule"`
	CheckInMargin         int64  `json:"checkin_margin"`
	MaxRuntime            int64  `json:"max_runtime"`
	Timezone              string `json:"timezone"`
	FailureIssueThreshold int64  `json:"failure_issue_threshold"`
	RecoveryThreshold     int64  `json:"recovery_threshold"`
}

func decodeConfig(header string) *sentry.MonitorConfig {
	if header == "" {
		return nil
	}
	var w wireConfig
	if err := json.Unmarshal([]byte(header), &w); err != nil || w.Schedule == nil {
		debuglog.Printf("sentryasynq: invalid monitor config header %q", header)
		return nil
	}
	config := &sentry.MonitorConfig{
		CheckInMargin:         w.CheckInMargin,
		MaxRuntime:            w.MaxRuntime,
		Timezone:              w.Timezone,
		FailureIssueThreshold: w.FailureIssueThreshold,
		RecoveryThreshold:     w.RecoveryThreshold,
	}
	var err error
	if w.Schedule.Type == "interval" {
		var value int64
		err = json.Unmarshal(w.Schedule.Value, &value)
		config.Schedule = sentry.IntervalSchedule(value, sentry.MonitorScheduleUnit(w.Schedule.Unit))
	} else {
		var value string
		err = json.Unmarshal(w.Schedule.Value, &value)
		config.Schedule = sentry.CrontabSchedule(value)
	}
	if err != nil {
		debuglog.Printf("sentryasynq: invalid monitor config header %q", header)
		return nil
	}
	return config
}
