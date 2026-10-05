package sentrycron

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMonitorConfig(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	fixed := time.FixedZone("UTC+5:30", 5*60*60+30*60)
	dayEvens := "1,3,5,7,9,11,13,15,17,19,21,23,25,27,29,31"

	tests := []struct {
		spec     string
		loc      *time.Location
		schedule sentry.MonitorSchedule
		timezone string
	}{
		{"0 3 * * *", time.UTC, sentry.CrontabSchedule("0 3 * * *"), "UTC"},
		{"0 3 * * ?", time.Local, sentry.CrontabSchedule("0 3 * * *"), "America/New_York"},
		{"0 3 * * *", fixed, nil, ""},
		{"0 3 * * *", time.FixedZone("", 0), sentry.CrontabSchedule("0 3 * * *"), "UTC"},
		{"CRON_TZ=Mars/Olympus 0 3 * * *", time.UTC, nil, ""},
		{"0 0 */2 * 1", time.UTC, sentry.CrontabSchedule("0 0 " + dayEvens + " * 1"), "UTC"},
		{"0 0 1 * */2", time.UTC, sentry.CrontabSchedule("0 0 1 * 0,2,4,6"), "UTC"},
		{"0 0 */2 * *", time.UTC, sentry.CrontabSchedule("0 0 */2 * *"), "UTC"},
		{"0 0 */1 * 1", time.UTC, sentry.CrontabSchedule("0 0 */1 * 1"), "UTC"},
		{"@every 1h", time.UTC, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitHour), "UTC"},
		{"@every 48h", time.UTC, sentry.IntervalSchedule(2, sentry.MonitorScheduleUnitDay), "UTC"},
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
			if tt.timezone == "America/New_York" && runtime.GOOS == "windows" {
				t.Skip("time.Local ignores TZ on Windows")
			}
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

func TestLocalName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("time.Local comes from the registry on Windows")
	}
	dir := t.TempDir()
	zone := filepath.Join(dir, "zoneinfo", "America", "Toronto")
	require.NoError(t, os.MkdirAll(filepath.Dir(zone), 0o755))
	require.NoError(t, os.WriteFile(zone, nil, 0o600))
	link := filepath.Join(dir, "localtime")
	require.NoError(t, os.Symlink(zone, link))

	previous := localtimePath
	t.Cleanup(func() { localtimePath = previous })

	tests := []struct {
		name      string
		tz        *string
		localtime string
		want      string
	}{
		{"TZ name", ptr("Asia/Tokyo"), link, "Asia/Tokyo"},
		{"TZ with colon", ptr(":Europe/Paris"), link, "Europe/Paris"},
		{"empty TZ", ptr(""), link, "UTC"},
		{"TZ path", ptr(link), "", "America/Toronto"},
		{"localtime link", nil, link, "America/Toronto"},
		{"no localtime", nil, filepath.Join(dir, "missing"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TZ", "")
			if tt.tz == nil {
				require.NoError(t, os.Unsetenv("TZ"))
			} else {
				t.Setenv("TZ", *tt.tz)
			}
			localtimePath = tt.localtime
			assert.Equal(t, tt.want, localName())
		})
	}
}

func ptr(s string) *string { return &s }
