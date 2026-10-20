package sentrygocron

import (
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
	f := sentrytest.NewFixture(t, sentrytest.WithGlobal())

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	s, err := gocron.NewScheduler(
		gocron.WithLocation(tokyo),
		gocron.WithSchedulerMonitor(NewSchedulerMonitor(Options{
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

	poll := bySlug["poll"]
	require.Len(t, poll, 2)
	assert.Nil(t, poll[0].MonitorConfig)
}
