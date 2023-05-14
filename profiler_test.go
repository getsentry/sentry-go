package sentry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStart(t *testing.T) {
	stopFn := startProfiling()
	time.Sleep(250 * time.Millisecond)
	trace := stopFn()
	require.NotEmpty(t, trace.Samples)
	require.NotEmpty(t, trace.Stacks)
	require.NotEmpty(t, trace.Frames)
	require.NotEmpty(t, trace.ThreadMetadata)
	// TODO proper test
}
