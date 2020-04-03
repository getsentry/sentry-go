//+build windows

package sentry

import "runtime"

func osContext() map[string]interface{} {
	return map[string]interface{}{
		"name": runtime.GOOS,
	}
}
