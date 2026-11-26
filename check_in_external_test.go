package sentry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
)

func checkInEvents(t *testing.T, f *sentrytest.Fixture) []*sentry.MockCheckIn {
	t.Helper()
	f.Flush()
	return f.Transport.CheckIns()
}

func TestWithMonitor(t *testing.T) {
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
				err := sentry.WithMonitor(f.Context, "my-job", tt.monitorConfig, func(context.Context) error {
					calls++
					time.Sleep(3 * time.Second)
					return tt.fnErr
				})
				assert.Equal(t, 1, calls)
				assert.Equal(t, tt.fnErr, err)

				events := checkInEvents(t, f)
				require.Len(t, events, 2)

				start, end := events[0], events[1]
				assert.Equal(t, "my-job", start.MonitorSlug)
				assert.Equal(t, sentry.CheckInStatusInProgress, start.Status)
				assert.Zero(t, start.Duration)
				var wantConfig json.RawMessage
				if tt.monitorConfig != nil {
					wantConfig, _ = json.Marshal(tt.monitorConfig)
				}
				assert.Equal(t, wantConfig, start.MonitorConfig)

				assert.Equal(t, start.ID, end.ID)
				assert.Equal(t, "my-job", end.MonitorSlug)
				assert.Equal(t, tt.wantStatus, end.Status)
				assert.Equal(t, 3.0, end.Duration)
				assert.Nil(t, end.MonitorConfig)
			})
		})
	}
}

func TestWithMonitorPanic(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		assert.PanicsWithValue(t, "boom", func() {
			_ = sentry.WithMonitor(f.Context, "my-job", nil, func(context.Context) error {
				time.Sleep(time.Second)
				panic("boom")
			})
		})

		events := checkInEvents(t, f)
		require.Len(t, events, 2)
		assert.Equal(t, sentry.CheckInStatusInProgress, events[0].Status)
		assert.Equal(t, events[0].ID, events[1].ID)
		assert.Equal(t, sentry.CheckInStatusError, events[1].Status)
		assert.Equal(t, 1.0, events[1].Duration)
	})
}

func TestWithMonitorTrace(t *testing.T) {
	t.Parallel()
	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		ctx := sentry.ContextWithClient(context.Background(), f.Client)
		for range 2 {
			_ = sentry.WithMonitor(ctx, "my-job", nil, func(context.Context) error { return nil })
		}

		events := checkInEvents(t, f)
		require.Len(t, events, 4)
		trace := func(i int) any { return events[i].Contexts["trace"]["trace_id"] }
		assert.Equal(t, trace(0), trace(1))
		assert.Equal(t, trace(2), trace(3))
		assert.NotEqual(t, trace(0), trace(2))
		for _, event := range events {
			assert.NotEmpty(t, event.Contexts["trace"]["span_id"])
		}
	})
}
