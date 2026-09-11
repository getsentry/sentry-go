package sentry_test

import (
	"testing"
	"testing/synctest"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Meter_ExceedBatchSize(t *testing.T) {
	t.Parallel()

	sentrytest.Run(t, func(t *testing.T, fixture *sentrytest.Fixture) {
		meter := sentry.NewMeter(fixture.Context)
		for i := 0; i < 99; i++ {
			meter.Count("test.count", 1)
		}

		// Let the worker settle below the batch threshold without advancing time.
		synctest.Wait()
		require.Empty(t, fixture.Events())

		meter.Count("test.count", 1)
		synctest.Wait()

		// Reaching the threshold must deliver the batch without an explicit flush.
		events := fixture.Events()
		require.Len(t, events, 1)
		assert.Equal(t, "trace_metric", events[0].Type)
		assert.Len(t, events[0].Metrics, 100)
	})
}
