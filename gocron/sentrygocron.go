// Package sentrygocron reports github.com/go-co-op/gocron/v2 jobs to Sentry
// Crons.
package sentrygocron

import (
	"context"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/cronspec"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
)

// Options configures how jobs are monitored.
type Options struct {
	// Location is the scheduler's time zone, as set with gocron.WithLocation.
	// Defaults to time.Local, like gocron.
	Location *time.Location
	// MonitorConfig sets the monitor fields other than the schedule and
	// time zone, such as CheckInMargin and MaxRuntime. It may be nil.
	MonitorConfig *sentry.MonitorConfig
	// Exclude lists job names that are not monitored.
	Exclude []string
	// ExcludePatterns lists patterns of job names that are not monitored.
	ExcludePatterns []*regexp.Regexp
}

// NewSchedulerMonitor returns a gocron.SchedulerMonitor that reports every
// run of the scheduler's jobs as check-ins, using the job name as the monitor
// slug. Check-ins use the client and scope carried by ctx. Pass it to
// gocron.WithSchedulerMonitor.
//
// The monitor is created or updated with the job's schedule. Only cron and
// duration jobs whose schedule Sentry can represent are monitored.
//
// Jobs without a name set by gocron.WithName are named after their function.
// Jobs running an anonymous function are not monitored, since that name
// changes with the code around it. Jobs sharing a name are not monitored,
// since they would report to the same monitor.
func NewSchedulerMonitor(ctx context.Context, options Options) gocron.SchedulerMonitor {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Location == nil {
		options.Location = time.Local
	}
	return &monitor{
		ctx:     ctx,
		options: options,
		names:   make(map[string]map[uuid.UUID]bool),
		runs:    make(map[uuid.UUID][]run),
	}
}

var anonymousFunc = regexp.MustCompile(`\.func\d+(\.\d+)*$`)

type run struct {
	slug  string
	id    *sentry.EventID
	start time.Time
}

type monitor struct {
	ctx     context.Context
	options Options

	mu    sync.Mutex
	names map[string]map[uuid.UUID]bool
	runs  map[uuid.UUID][]run
}

func (m *monitor) JobRegistered(job gocron.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ids := range m.names {
		delete(ids, job.ID())
	}
	if m.names[job.Name()] == nil {
		m.names[job.Name()] = make(map[uuid.UUID]bool)
	}
	m.names[job.Name()][job.ID()] = true
}

func (m *monitor) JobUnregistered(job gocron.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.names[job.Name()], job.ID())
}

func (m *monitor) JobStarted(job gocron.Job) {
	slug := job.Name()
	if !m.monitored(job) {
		return
	}
	config := m.monitorConfig(job.Schedule())
	if config == nil {
		debuglog.Printf("sentrygocron: job %q has a schedule Sentry can't represent, so it is not monitored", slug)
		return
	}
	id := sentry.CaptureCheckIn(m.ctx, &sentry.CheckIn{
		MonitorSlug: slug,
		Status:      sentry.CheckInStatusInProgress,
	}, config)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[job.ID()] = append(m.runs[job.ID()], run{slug, id, time.Now()})
}

func (m *monitor) monitored(job gocron.Job) bool {
	name := job.Name()
	if slices.Contains(m.options.Exclude, name) || slices.ContainsFunc(m.options.ExcludePatterns, func(re *regexp.Regexp) bool {
		return re.MatchString(name)
	}) {
		return false
	}
	if anonymousFunc.MatchString(name) {
		debuglog.Printf("sentrygocron: job %q runs an anonymous function; set a name with gocron.WithName to monitor it", name)
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.names[name]) > 1 {
		debuglog.Printf("sentrygocron: job name %q is used by more than one job; give each a unique name to monitor them", name)
		return false
	}
	return true
}

func (m *monitor) monitorConfig(schedule gocron.JobSchedule) *sentry.MonitorConfig {
	switch s := schedule.(type) {
	case gocron.CronJobSchedule:
		return cronspec.MonitorConfig(s.Crontab, m.options.Location, m.options.MonitorConfig)
	case gocron.DurationJobSchedule:
		return cronspec.MonitorConfig("@every "+s.Duration.String(), m.options.Location, m.options.MonitorConfig)
	}
	return nil
}

func (m *monitor) finish(job gocron.Job, status sentry.CheckInStatus) {
	m.mu.Lock()
	runs := m.runs[job.ID()]
	if len(runs) == 0 {
		m.mu.Unlock()
		return
	}
	r := runs[0]
	if len(runs) == 1 {
		delete(m.runs, job.ID())
	} else {
		m.runs[job.ID()] = runs[1:]
	}
	m.mu.Unlock()

	checkIn := &sentry.CheckIn{
		MonitorSlug: r.slug,
		Status:      status,
		Duration:    time.Since(r.start),
	}
	if r.id != nil {
		checkIn.ID = *r.id
	}
	sentry.CaptureCheckIn(m.ctx, checkIn, nil)
}

func (m *monitor) JobCompleted(job gocron.Job) { m.finish(job, sentry.CheckInStatusOK) }

func (m *monitor) JobFailed(job gocron.Job, _ error) { m.finish(job, sentry.CheckInStatusError) }

func (m *monitor) SchedulerStarted()                                   {}
func (m *monitor) SchedulerStopped()                                   {}
func (m *monitor) SchedulerShutdown()                                  {}
func (m *monitor) JobRunning(gocron.Job)                               {}
func (m *monitor) JobExecutionTime(gocron.Job, time.Duration)          {}
func (m *monitor) JobSchedulingDelay(gocron.Job, time.Time, time.Time) {}
func (m *monitor) ConcurrencyLimitReached(string, gocron.Job)          {}
