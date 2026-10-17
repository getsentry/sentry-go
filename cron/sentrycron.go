// Package sentrycron reports github.com/robfig/cron/v3 jobs to Sentry Crons.
package sentrycron

import (
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/cronspec"
	"github.com/robfig/cron/v3"
)

// AddFunc adds fn to c with spec and reports each run as check-ins for the
// monitor slug. The monitor is created or updated with spec as its schedule
// when Sentry can represent it. config sets the other monitor fields and may
// be nil.
//
// The monitor uses the cron's time zone. When that is time.Local, the IANA name
// comes from $TZ or /etc/localtime. If no IANA name can be found, only
// check-ins are sent and the monitor's schedule must be set in Sentry.
func AddFunc(c *cron.Cron, spec, slug string, fn func() error, config *sentry.MonitorConfig) (cron.EntryID, error) {
	monitorConfig := cronspec.MonitorConfig(spec, c.Location(), config)
	return c.AddFunc(spec, func() {
		_ = sentry.WithMonitor(slug, monitorConfig, fn)
	})
}
