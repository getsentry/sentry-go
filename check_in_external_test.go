package sentry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
)

func checkInEvents(t *testing.T, f *sentrytest.Fixture) []*sentry.Event {
	t.Helper()
	f.Flush()
	events := f.Events()
	for _, event := range events {
		require.Equal(t, "check_in", event.Type)
		require.NotNil(t, event.CheckIn)
	}
	return events
}

func TestWithMonitorContext(t *testing.T) {
	t.Parallel()

	errJob := errors.New("job failed")
	monitorConfig := &sentry.MonitorConfig{
		Schedule:      sentry.CrontabSchedule("0 3 * * *"),
		CheckInMargin: 5,
		MaxRuntime:    30,
		Timezone:      "UTC",
	}

	tests := []struct {
		name          string
		monitorConfig *sentry.MonitorConfig
		fnErr         error
		wantStatus    sentry.CheckInStatus
	}{
		{
			name:          "ok",
			monitorConfig: monitorConfig,
			wantStatus:    sentry.CheckInStatusOK,
		},
		{
			name:          "error",
			monitorConfig: monitorConfig,
			fnErr:         errJob,
			wantStatus:    sentry.CheckInStatusError,
		},
		{
			name:       "nil config",
			wantStatus: sentry.CheckInStatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				calls := 0
				err := sentry.WithMonitor(f.Context, "my-job", tt.monitorConfig, func() error {
					calls++
					time.Sleep(3 * time.Second)
					return tt.fnErr
				})
				assert.Equal(t, 1, calls)
				assert.Equal(t, tt.fnErr, err)

				events := checkInEvents(t, f)
				require.Len(t, events, 2)

				start, end := events[0], events[1]
				assert.Equal(t, "my-job", start.CheckIn.MonitorSlug)
				assert.Equal(t, sentry.CheckInStatusInProgress, start.CheckIn.Status)
				assert.Zero(t, start.CheckIn.Duration)
				assert.Equal(t, tt.monitorConfig, start.MonitorConfig)

				assert.Equal(t, start.CheckIn.ID, end.CheckIn.ID)
				assert.Equal(t, "my-job", end.CheckIn.MonitorSlug)
				assert.Equal(t, tt.wantStatus, end.CheckIn.Status)
				assert.Equal(t, 3*time.Second, end.CheckIn.Duration)
				assert.Nil(t, end.MonitorConfig)
			})
		})
	}
}

func TestWithMonitorPanic(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		assert.PanicsWithValue(t, "boom", func() {
			_ = sentry.WithMonitor(f.Context, "my-job", nil, func() error {
				time.Sleep(time.Second)
				panic("boom")
			})
		})

		events := checkInEvents(t, f)
		require.Len(t, events, 2)
		assert.Equal(t, sentry.CheckInStatusInProgress, events[0].CheckIn.Status)
		assert.Equal(t, events[0].CheckIn.ID, events[1].CheckIn.ID)
		assert.Equal(t, sentry.CheckInStatusError, events[1].CheckIn.Status)
		assert.Equal(t, time.Second, events[1].CheckIn.Duration)
	})
}

func TestWithMonitor(t *testing.T) {
	f := sentrytest.NewFixture(t, sentrytest.WithGlobal())

	monitorConfig := &sentry.MonitorConfig{Schedule: sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitHour)}
	err := sentry.WithMonitor(context.Background(), "my-job", monitorConfig, func() error { return nil })
	require.NoError(t, err)

	events := checkInEvents(t, f)
	require.Len(t, events, 2)
	assert.Equal(t, monitorConfig, events[0].MonitorConfig)
	assert.Equal(t, sentry.CheckInStatusOK, events[1].CheckIn.Status)
}
