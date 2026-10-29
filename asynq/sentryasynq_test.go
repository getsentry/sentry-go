package sentryasynq

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type provider []*asynq.PeriodicTaskConfig

func (p provider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) { return p, nil }

func TestConfigProvider(t *testing.T) {
	t.Parallel()
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	p := NewConfigProvider(provider{
		{Cronspec: "0 3 * * *", Task: asynq.NewTask("report:daily", nil)},
		{Cronspec: "@every 30s", Task: asynq.NewTask("ping", nil)},
		{Cronspec: "@every 1h", Task: asynq.NewTask("cache:refresh", nil)},
		{Cronspec: "@every 1h", Task: asynq.NewTask("tenant:sync", []byte("a"))},
		{Cronspec: "@every 2h", Task: asynq.NewTask("tenant:sync", []byte("b"))},
		{Cronspec: "@every 1h", Task: asynq.NewTask("email:digest", nil, asynq.Queue("low"))},
		{Cronspec: "@every 1h", Task: asynq.NewTaskWithHeaders("audit", nil, map[string]string{"k": "v"})},
		{Cronspec: "@every 1h", Task: asynq.NewTaskWithHeaders("cleanup", nil, map[string]string{"k": "v"}, asynq.Queue("low"))},
	}, Options{
		Location:        tokyo,
		MonitorConfig:   &sentry.MonitorConfig{MaxRuntime: 10},
		Exclude:         []string{"cache:refresh"},
		ExcludePatterns: []*regexp.Regexp{regexp.MustCompile(`^nothing`)},
	})
	configs, err := p.GetConfigs()
	require.NoError(t, err)

	headers := make(map[string]map[string]string)
	for _, c := range configs {
		headers[c.Task.Type()+string(c.Task.Payload())] = c.Task.Headers()
	}
	assert.Equal(t, map[string]string{
		slugHeader:   "report:daily",
		configHeader: `{"schedule":{"type":"crontab","value":"0 3 * * *"},"max_runtime":10,"timezone":"Asia/Tokyo"}`,
	}, headers["report:daily"])
	assert.Nil(t, headers["ping"])
	assert.Nil(t, headers["cache:refresh"])
	assert.Nil(t, headers["tenant:synca"])
	assert.Nil(t, headers["tenant:syncb"])
	assert.Nil(t, headers["email:digest"])
	assert.Equal(t, "audit", headers["audit"][slugHeader])
	assert.Equal(t, "v", headers["audit"]["k"])
	assert.Equal(t, map[string]string{"k": "v"}, headers["cleanup"])
}

func TestWithMonitorCopiesTask(t *testing.T) {
	t.Parallel()
	task := asynq.NewTaskWithHeaders("sync", []byte("p"), map[string]string{"k": "v"})
	hourly := WithMonitor(task, "sync-hourly", "@every 1h", Options{})
	daily := WithMonitor(task, "sync-daily", "@daily", Options{})

	assert.Equal(t, map[string]string{"k": "v"}, task.Headers())
	assert.Equal(t, "sync-hourly", hourly.Headers()[slugHeader])
	assert.Equal(t, "sync-daily", daily.Headers()[slugHeader])
	assert.Equal(t, "v", daily.Headers()["k"])
	assert.Equal(t, []byte("p"), daily.Payload())
	assert.Empty(t, WithMonitor(task, "sync-fast", "@every 30s", Options{}).Headers()[configHeader])
}

func TestMiddleware(t *testing.T) {
	t.Parallel()
	f := sentrytest.NewFixture(t)
	ctx := f.Context
	task := WithMonitor(asynq.NewTask("report:daily", nil), "daily-report", "@every 2h", Options{})

	var ran bool
	handler := Middleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		ran = true
		return errors.New("failed")
	}))
	err := handler.ProcessTask(ctx, task)
	assert.EqualError(t, err, "failed")
	assert.True(t, ran)

	f.Flush()
	events := f.Events()
	require.Len(t, events, 2)
	assert.Equal(t, "daily-report", events[0].CheckIn.MonitorSlug)
	assert.Equal(t, sentry.CheckInStatusInProgress, events[0].CheckIn.Status)
	assert.Equal(t, &sentry.MonitorConfig{
		Schedule: sentry.IntervalSchedule(2, sentry.MonitorScheduleUnitHour),
		Timezone: "UTC",
	}, events[0].MonitorConfig)
	assert.Equal(t, sentry.CheckInStatusError, events[1].CheckIn.Status)
	assert.Equal(t, events[0].CheckIn.ID, events[1].CheckIn.ID)
}

func TestMiddlewareUnmonitored(t *testing.T) {
	t.Parallel()
	f := sentrytest.NewFixture(t)
	ctx := f.Context
	handler := Middleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))
	require.NoError(t, handler.ProcessTask(ctx, asynq.NewTask("adhoc", nil)))
	require.NoError(t, handler.ProcessTask(ctx, asynq.NewTaskWithHeaders("bad", nil, map[string]string{
		slugHeader:   "bad",
		configHeader: "{",
	})))

	f.Flush()
	events := f.Events()
	require.Len(t, events, 2)
	assert.Equal(t, "bad", events[0].CheckIn.MonitorSlug)
	assert.Nil(t, events[0].MonitorConfig)
	assert.Equal(t, sentry.CheckInStatusOK, events[1].CheckIn.Status)
}
