package testutils

import (
	"context"
	"os"
	"time"
)

func IsCI() bool {
	return os.Getenv("CI") != ""
}

func FlushTimeout() time.Duration {
	if IsCI() {
		// CI is very overloaded so we need to allow for a long wait time.
		return 5 * time.Second
	}

	return time.Second
}

// FlushWithTimeout flushes f, waiting for at most timeout.
func FlushWithTimeout(f interface{ FlushWithContext(context.Context) bool }, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return f.FlushWithContext(ctx)
}
