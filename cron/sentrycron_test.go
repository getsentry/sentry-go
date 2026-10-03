package sentrycron

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMonitorConfig(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	tests := []struct {
		spec     string
		loc      *time.Location
		schedule sentry.MonitorSchedule
		timezone string
	}{
		{"0 3 * * *", time.UTC, sentry.CrontabSchedule("0 3 * * *"), "UTC"},
		{"0 3 * * ?", time.Local, sentry.CrontabSchedule("0 3 * * *"), ""},
		{"30 0 3 * * *", time.UTC, sentry.CrontabSchedule("0 3 * * *"), "UTC"},
		{"*/10 * * * * *", time.UTC, nil, ""},
		{"@every 90m", time.UTC, sentry.IntervalSchedule(90, sentry.MonitorScheduleUnitMinute), "UTC"},
		{"@every 30s", time.UTC, nil, ""},
		{"@daily", tokyo, sentry.CrontabSchedule("@daily"), "Asia/Tokyo"},
		{"@midnight", time.UTC, sentry.CrontabSchedule("0 0 * * *"), "UTC"},
		{"CRON_TZ=Asia/Tokyo 0 6 * * *", time.UTC, sentry.CrontabSchedule("0 6 * * *"), "Asia/Tokyo"},
		{"TZ=Europe/Paris @hourly", time.Local, sentry.CrontabSchedule("@hourly"), "Europe/Paris"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			config := newMonitorConfig(tt.spec, tt.loc, &sentry.MonitorConfig{MaxRuntime: 30})
			if tt.schedule == nil {
				assert.Nil(t, config)
				return
			}
			require.NotNil(t, config)
			assert.Equal(t, tt.schedule, config.Schedule)
			assert.Equal(t, tt.timezone, config.Timezone)
			assert.Equal(t, int64(30), config.MaxRuntime)
		})
	}
}

func TestAddFunc(t *testing.T) {
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{Transport: transport})
	require.NoError(t, err)
	sentry.CurrentHub().BindClient(client)

	c := cron.New(cron.WithLocation(time.UTC))
	id, err := AddFunc(c, "0 3 * * *", "nightly", func() error { return errors.New("failed") }, nil)
	require.NoError(t, err)
	c.Entry(id).Job.Run()

	events := transport.Events()
	require.Len(t, events, 2)
	assert.Equal(t, sentry.CheckInStatusInProgress, events[0].CheckIn.Status)
	assert.Equal(t, sentry.CrontabSchedule("0 3 * * *"), events[0].MonitorConfig.Schedule)
	assert.Equal(t, "UTC", events[0].MonitorConfig.Timezone)
	assert.Equal(t, sentry.CheckInStatusError, events[1].CheckIn.Status)
}
