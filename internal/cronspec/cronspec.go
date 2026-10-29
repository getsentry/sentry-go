// Package cronspec converts github.com/robfig/cron/v3 specs to Sentry monitor
// configs. It is shared by the integrations for schedulers that use that
// parser.
package cronspec

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// MonitorConfig returns base with the schedule and time zone of spec, which
// runs in loc unless it has a TZ= or CRON_TZ= prefix. It returns nil when
// Sentry cannot represent the schedule, or when loc has no IANA name.
//
// When loc is time.Local, the IANA name comes from $TZ or /etc/localtime.
func MonitorConfig(spec string, loc *time.Location, base *sentry.MonitorConfig) *sentry.MonitorConfig {
	var config sentry.MonitorConfig
	if base != nil {
		config = *base
	}
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
		prefix, rest, _ := strings.Cut(spec, " ")
		_, name, _ := strings.Cut(prefix, "=")
		spec = strings.TrimSpace(rest)
		config.Timezone = ""
		var err error
		if loc, err = time.LoadLocation(name); err != nil {
			return noTimezone(name)
		}
	}
	config.Schedule = schedule(spec)
	if config.Schedule == nil {
		return nil
	}
	if config.Timezone == "" {
		tz, ok := timezone(loc)
		if !ok {
			return noTimezone(loc.String())
		}
		config.Timezone = tz
	}
	return &config
}

// noTimezone skips the monitor config, since the schedule would be read as UTC.
func noTimezone(name string) *sentry.MonitorConfig {
	debuglog.Printf("sentry: no IANA name for time zone %q; sending check-ins without a monitor config", name)
	return nil
}

// localtimePath is a variable so tests can point it at a fake zoneinfo link.
var localtimePath = "/etc/localtime"

// timezone returns the IANA name of loc, which is the only form Sentry accepts.
func timezone(loc *time.Location) (string, bool) {
	name := loc.String()
	if loc == time.Local {
		name = localName()
	}
	if name != "" && name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name, true
		}
	}
	// A zone that is always UTC, such as time.Local without zone data.
	year := time.Now().Year()
	for _, t := range []time.Time{time.Date(year, 1, 1, 0, 0, 0, 0, loc), time.Date(year, 7, 1, 0, 0, 0, 0, loc)} {
		if _, offset := t.Zone(); offset != 0 {
			return "", false
		}
	}
	return "UTC", true
}

// localName follows how the time package picks time.Local on Unix. Windows
// reads the zone from the registry, which has no IANA name.
func localName() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	path := localtimePath
	if tz, ok := os.LookupEnv("TZ"); ok {
		if tz == "" {
			return "UTC"
		}
		tz = strings.TrimPrefix(tz, ":")
		if !filepath.IsAbs(tz) {
			return tz
		}
		path = tz
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if i := strings.LastIndex(target, "zoneinfo/"); i >= 0 {
		return target[i+len("zoneinfo/"):]
	}
	return ""
}

func schedule(spec string) sentry.MonitorSchedule {
	switch {
	case spec == "@midnight":
		return sentry.CrontabSchedule("0 0 * * *")
	case strings.HasPrefix(spec, "@every "):
		d, err := time.ParseDuration(strings.TrimPrefix(spec, "@every "))
		if err != nil || d < time.Minute || d%time.Minute != 0 {
			return nil
		}
		switch {
		case d%(24*time.Hour) == 0:
			return sentry.IntervalSchedule(int64(d/(24*time.Hour)), sentry.MonitorScheduleUnitDay)
		case d%time.Hour == 0:
			return sentry.IntervalSchedule(int64(d/time.Hour), sentry.MonitorScheduleUnitHour)
		}
		return sentry.IntervalSchedule(int64(d/time.Minute), sentry.MonitorScheduleUnitMinute)
	case strings.HasPrefix(spec, "@"):
		return sentry.CrontabSchedule(spec)
	}
	fields := strings.Fields(strings.ReplaceAll(spec, "?", "*"))
	if len(fields) == 6 {
		// Seconds field, from cron.WithSeconds. Only a fixed second can be dropped.
		if _, err := strconv.Atoi(fields[0]); err != nil {
			return nil
		}
		fields = fields[1:]
	}
	if len(fields) != 5 {
		return nil
	}
	// robfig/cron treats a stepped `*/n` day field as restricted and matches
	// either day field, while Sentry treats any field starting with `*` as
	// unrestricted and matches both. Listing the days keeps robfig's meaning.
	if fields[2] != "*" && fields[4] != "*" {
		var ok bool
		if fields[2], ok = expandStep(fields[2], 1, 31); !ok {
			return nil
		}
		if fields[4], ok = expandStep(fields[4], 0, 6); !ok {
			return nil
		}
	}
	return sentry.CrontabSchedule(strings.Join(fields, " "))
}

// expandStep replaces each `*/n` part of field with the values it matches.
func expandStep(field string, minValue, maxValue int) (string, bool) {
	parts := strings.Split(field, ",")
	for i, part := range parts {
		stepStr, found := strings.CutPrefix(part, "*/")
		if !found {
			continue
		}
		step, err := strconv.Atoi(stepStr)
		if err != nil || step < 1 {
			return "", false
		}
		if step == 1 {
			// robfig/cron still treats `*/1` as unrestricted.
			continue
		}
		var values []string
		for v := minValue; v <= maxValue; v += step {
			values = append(values, strconv.Itoa(v))
		}
		parts[i] = strings.Join(values, ",")
	}
	return strings.Join(parts, ","), true
}
