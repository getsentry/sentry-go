package main

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func runTask(monitorSlug string, duration time.Duration, shouldFail bool) {
	checkinID := sentry.EventID("")
	if id := sentry.CaptureCheckIn(
		context.Background(),
		&sentry.CheckIn{
			MonitorSlug: monitorSlug,
			Status:      sentry.CheckInStatusInProgress,
		},
		&sentry.MonitorConfig{
			Schedule:      sentry.CrontabSchedule("* * * * *"),
			MaxRuntime:    2,
			CheckInMargin: 1,
		},
	); id != nil {
		checkinID = *id
	} else {
		fmt.Printf("Task[monitor_slug=%s] was not accepted by Sentry\n", monitorSlug)
	}
	task := fmt.Sprintf("Task[monitor_slug=%s,id=%s]", monitorSlug, checkinID)
	fmt.Printf("Task started: %s\n", task)

	time.Sleep(duration)

	var status sentry.CheckInStatus
	if shouldFail {
		status = sentry.CheckInStatusError
	} else {
		status = sentry.CheckInStatusOK
	}

	sentry.CaptureCheckIn(
		context.Background(),
		&sentry.CheckIn{
			ID:          checkinID,
			MonitorSlug: monitorSlug,
			Status:      status,
		},
		nil,
	)
	fmt.Printf("Task finished: %s; Status: %s\n", task, status)
}

// runMonitoredTask does the same as runTask using sentry.WithMonitor, which
// sends the in_progress and final check-ins around the job and creates or
// updates the monitor from the given config.
func runMonitoredTask(monitorSlug string, duration time.Duration) {
	err := sentry.WithMonitor(context.Background(), monitorSlug, &sentry.MonitorConfig{
		Schedule:      sentry.CrontabSchedule("* * * * *"),
		MaxRuntime:    2,
		CheckInMargin: 1,
	}, func() error {
		time.Sleep(duration)
		return nil
	})
	fmt.Printf("Task finished: monitor_slug=%s; err=%v\n", monitorSlug, err)
}

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:   "",
		Debug: true,
	})

	// Start a task that runs every minute and always succeeds
	go func() {
		for {
			go runTask("sentry-go-periodic-task-success", time.Second, false)
			time.Sleep(time.Minute)
		}
	}()

	time.Sleep(3 * time.Second)

	// Start a task that runs every minute and fails every second time
	go func() {
		shouldFail := true
		for {
			go runTask("sentry-go-periodic-task-sometimes-fail", 2*time.Second, shouldFail)
			time.Sleep(time.Minute)
			shouldFail = !shouldFail
		}
	}()

	time.Sleep(3 * time.Second)

	// Start a task that runs every minute, wrapped with sentry.WithMonitor
	go func() {
		for {
			go runMonitoredTask("sentry-go-periodic-task-with-monitor", time.Second)
			time.Sleep(time.Minute)
		}
	}()

	select {}
}
