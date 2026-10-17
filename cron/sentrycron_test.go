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

func TestAddFunc(t *testing.T) {
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{Transport: transport})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	previous := hub.Client()
	t.Cleanup(func() { hub.BindClient(previous) })
	hub.BindClient(client)

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
