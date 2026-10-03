// Package sentrycron reports github.com/robfig/cron/v3 jobs to Sentry Crons.
package sentrycron

import (
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/robfig/cron/v3"
)

// AddFunc adds fn to c with spec and reports each run as check-ins for the
// monitor slug. The monitor is created or updated with spec as its schedule
// when Sentry can represent it. config sets the other monitor fields and may
// be nil.
func AddFunc(c *cron.Cron, spec, slug string, fn func() error, config *sentry.MonitorConfig) (cron.EntryID, error) {
	monitorConfig := newMonitorConfig(spec, c.Location(), config)
	return c.AddFunc(spec, func() {
		_ = sentry.WithMonitor(slug, monitorConfig, fn)
	})
}

func newMonitorConfig(spec string, loc *time.Location, base *sentry.MonitorConfig) *sentry.MonitorConfig {
	var config sentry.MonitorConfig
	if base != nil {
		config = *base
	}
	if config.Timezone == "" && loc != time.Local {
		config.Timezone = loc.String()
	}
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
		prefix, rest, _ := strings.Cut(spec, " ")
		_, config.Timezone, _ = strings.Cut(prefix, "=")
		spec = strings.TrimSpace(rest)
	}
	config.Schedule = schedule(spec)
	if config.Schedule == nil {
		return nil
	}
	return &config
}

func schedule(spec string) sentry.MonitorSchedule {
	switch {
	case spec == "@midnight":
		return sentry.CrontabSchedule("0 0 * * *")
	case strings.HasPrefix(spec, "@every "):
		d, err := time.ParseDuration(strings.TrimPrefix(spec, "@every "))
		if err != nil || d < time.Minute || d%time.Minute != 0 {
			return nil
		}
		return sentry.IntervalSchedule(int64(d/time.Minute), sentry.MonitorScheduleUnitMinute)
	case strings.HasPrefix(spec, "@"):
		return sentry.CrontabSchedule(spec)
	}
	fields := strings.Fields(strings.ReplaceAll(spec, "?", "*"))
	if len(fields) == 6 {
		// Seconds field, from cron.WithSeconds. Only a fixed second can be dropped.
		if _, err := strconv.Atoi(fields[0]); err != nil {
			return nil
		}
		fields = fields[1:]
	}
	if len(fields) != 5 {
		return nil
	}
	return sentry.CrontabSchedule(strings.Join(fields, " "))
}
