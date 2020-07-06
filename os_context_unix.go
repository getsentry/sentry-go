//+build !windows

package sentry

import (
	"bytes"
	"runtime"

	"golang.org/x/sys/unix"
)

func osContext() map[string]interface{} {
	ctx := map[string]interface{}{
		"name": runtime.GOOS,
	}

	var name unix.Utsname
	if err := unix.Uname(&name); err != nil {
		return ctx
	}

	ctx["version"] = string(name.Release[:bytes.IndexByte(name.Release[:], 0)])
	ctx["kernel_version"] = string(name.Version[:bytes.IndexByte(name.Version[:], 0)])
	return ctx
}
