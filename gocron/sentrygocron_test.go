package sentrygocron

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/go-co-op/gocron/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMonitor(t *testing.T) {
	f := sentrytest.NewFixture(t)

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	s, err := gocron.NewScheduler(
		gocron.WithLocation(tokyo),
		gocron.WithSchedulerMonitor(NewSchedulerMonitor(f.Context, Options{
			Location:        tokyo,
			MonitorConfig:   &sentry.MonitorConfig{MaxRuntime: 10},
			Exclude:         []string{"excluded"},
			ExcludePatterns: []*regexp.Regexp{regexp.MustCompile(`^tmp-`)},
		})),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Shutdown() })
	s.Start()

	add := func(def gocron.JobDefinition, fn func() error, opts ...gocron.JobOption) gocron.Job {
		job, err := s.NewJob(def, gocron.NewTask(fn), opts...)
		require.NoError(t, err)
		return job
	}
	ok := func() error { return nil }
	jobs := []gocron.Job{
		add(gocron.CronJob("0 3 * * *", false), ok, gocron.WithName("nightly")),
		add(gocron.DurationJob(90*time.Minute), func() error { return errors.New("failed") }, gocron.WithName("sync")),
		add(gocron.DurationJob(30*time.Second), ok, gocron.WithName("poll")),
		add(gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(4, 30, 0))), ok, gocron.WithName("daily")),
		add(gocron.DurationJob(time.Hour), ok, gocron.WithName("excluded")),
		add(gocron.DurationJob(time.Hour), ok, gocron.WithName("tmp-1")),
		add(gocron.DurationJob(time.Hour), ok),
		add(gocron.DurationJob(time.Hour), ok, gocron.WithName("shared")),
		add(gocron.DurationJob(2*time.Hour), ok, gocron.WithName("shared")),
	}
	for _, job := range jobs {
		require.NoError(t, job.RunNow())
	}
	require.Eventually(t, func() bool {
		f.Flush()
		return len(f.Events()) >= 6
	}, 5*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	f.Flush()

	bySlug := make(map[string][]*sentry.Event)
	for _, e := range f.Events() {
		bySlug[e.CheckIn.MonitorSlug] = append(bySlug[e.CheckIn.MonitorSlug], e)
	}
	require.Len(t, bySlug, 3)

	nightly := bySlug["nightly"]
	require.Len(t, nightly, 2)
	assert.Equal(t, sentry.CheckInStatusInProgress, nightly[0].CheckIn.Status)
	assert.Equal(t, &sentry.MonitorConfig{
		Schedule:   sentry.CrontabSchedule("0 3 * * *"),
		MaxRuntime: 10,
		Timezone:   "Asia/Tokyo",
	}, nightly[0].MonitorConfig)
	assert.Equal(t, sentry.CheckInStatusOK, nightly[1].CheckIn.Status)
	assert.Equal(t, nightly[0].CheckIn.ID, nightly[1].CheckIn.ID)

	sync := bySlug["sync"]
	require.Len(t, sync, 2)
	assert.Equal(t, sentry.IntervalSchedule(90, sentry.MonitorScheduleUnitMinute), sync[0].MonitorConfig.Schedule)
	assert.Equal(t, sentry.CheckInStatusError, sync[1].CheckIn.Status)

	daily := bySlug["daily"]
	require.Len(t, daily, 2)
	assert.Equal(t, sentry.CrontabSchedule("30 4 * * *"), daily[0].MonitorConfig.Schedule)
	assert.Equal(t, "Asia/Tokyo", daily[0].MonitorConfig.Timezone)

	assert.Empty(t, bySlug["poll"])
}

func TestMonitorConfig(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	at := func(hour, minute, second int) time.Time {
		return time.Date(0, 0, 0, hour, minute, second, 0, tokyo)
	}
	m := NewSchedulerMonitor(context.Background(), Options{Location: tokyo}).(*monitor)

	tests := []struct {
		name     string
		schedule gocron.JobSchedule
		want     string
	}{
		{"daily", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(3, 0, 0)}}, "0 3 * * *"},
		{"daily hours", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(9, 15, 0), at(17, 15, 0)}}, "15 9,17 * * *"},
		{"daily minutes", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(8, 45, 0), at(8, 0, 0)}}, "0,45 8 * * *"},
		{"daily grid", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(1, 0, 0), at(1, 30, 0), at(2, 0, 0), at(2, 30, 0)}}, "0,30 1,2 * * *"},
		{"daily duplicate times", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(3, 0, 0), at(3, 0, 0)}}, "0 3 * * *"},
		{"daily not a grid", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(1, 0, 0), at(2, 30, 0)}}, ""},
		{"daily seconds", gocron.DailyJobSchedule{Interval: 1, AtTimes: []time.Time{at(3, 0, 10)}}, ""},
		{"daily interval", gocron.DailyJobSchedule{Interval: 2, AtTimes: []time.Time{at(3, 0, 0)}}, ""},
		{"daily no times", gocron.DailyJobSchedule{Interval: 1}, ""},
		{"weekly", gocron.WeeklyJobSchedule{Interval: 1, DaysOfWeek: []time.Weekday{time.Friday, time.Monday}, AtTimes: []time.Time{at(6, 0, 0)}}, "0 6 * * 1,5"},
		{"weekly sunday", gocron.WeeklyJobSchedule{Interval: 1, DaysOfWeek: []time.Weekday{time.Sunday}, AtTimes: []time.Time{at(0, 0, 0)}}, "0 0 * * 0"},
		{"weekly interval", gocron.WeeklyJobSchedule{Interval: 2, DaysOfWeek: []time.Weekday{time.Monday}, AtTimes: []time.Time{at(6, 0, 0)}}, ""},
		{"weekly no days", gocron.WeeklyJobSchedule{Interval: 1, AtTimes: []time.Time{at(6, 0, 0)}}, ""},
		{"weekly not a grid", gocron.WeeklyJobSchedule{Interval: 1, DaysOfWeek: []time.Weekday{time.Monday}, AtTimes: []time.Time{at(6, 0, 0), at(7, 30, 0)}}, ""},
		{"monthly", gocron.MonthlyJobSchedule{Interval: 1, Days: []int{15, 1}, AtTimes: []time.Time{at(12, 5, 0)}}, "5 12 1,15 * *"},
		{"monthly from end", gocron.MonthlyJobSchedule{Interval: 1, Days: []int{1}, DaysFromEnd: []int{-1}, AtTimes: []time.Time{at(12, 0, 0)}}, ""},
		{"monthly interval", gocron.MonthlyJobSchedule{Interval: 3, Days: []int{1}, AtTimes: []time.Time{at(12, 0, 0)}}, ""},
		{"monthly seconds", gocron.MonthlyJobSchedule{Interval: 1, Days: []int{1}, AtTimes: []time.Time{at(12, 0, 30)}}, ""},
		{"random duration", gocron.DurationRandomJobSchedule{Min: time.Minute, Max: time.Hour}, ""},
		{"one time", gocron.OneTimeJobSchedule{StartAt: []time.Time{time.Now()}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := m.monitorConfig(tt.schedule)
			if tt.want == "" {
				assert.Nil(t, config)
				return
			}
			require.NotNil(t, config)
			assert.Equal(t, sentry.CrontabSchedule(tt.want), config.Schedule)
			assert.Equal(t, "Asia/Tokyo", config.Timezone)
		})
	}
}

func TestSchedulerMonitorJobRemovedWhileRunning(t *testing.T) {
	f := sentrytest.NewFixture(t)
	s, err := gocron.NewScheduler(gocron.WithSchedulerMonitor(NewSchedulerMonitor(f.Context, Options{})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Shutdown() })
	s.Start()

	release := make(chan struct{})
	job, err := s.NewJob(gocron.DurationJob(time.Hour), gocron.NewTask(func() { <-release }), gocron.WithName("slow"))
	require.NoError(t, err)
	require.NoError(t, job.RunNow())
	require.Eventually(t, func() bool {
		f.Flush()
		return len(f.Events()) == 1
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, s.RemoveJob(job.ID()))
	require.Eventually(t, func() bool { return len(s.Jobs()) == 0 }, 5*time.Second, 10*time.Millisecond)
	close(release)
	require.Eventually(t, func() bool {
		f.Flush()
		return len(f.Events()) == 2
	}, 5*time.Second, 10*time.Millisecond)
	events := f.Events()
	assert.Equal(t, sentry.CheckInStatusOK, events[1].CheckIn.Status)
	assert.Equal(t, events[0].CheckIn.ID, events[1].CheckIn.ID)
}
