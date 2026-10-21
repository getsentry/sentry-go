package sentry

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/util"
	exec "golang.org/x/sys/execabs"
)

func uuid() string {
	return util.GenerateEventID()
}

func fileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return err == nil
}

// monotonicTimeSince replaces uses of time.Now() to take into account the
// monotonic clock reading stored in start, such that duration = end - start is
// unaffected by changes in the system wall clock.
func monotonicTimeSince(start time.Time) (end time.Time) {
	return start.Add(time.Since(start))
}

// nolint: unused
func prettyPrint(data interface{}) {
	dbg, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(dbg))
}

// defaultRelease attempts to guess a default release for the currently running
// program.
func defaultRelease() (release string) {
	// Return first non-empty environment variable known to hold release info, if any.
	envs := []string{
		"SENTRY_RELEASE",
		"HEROKU_BUILD_COMMIT",
		"HEROKU_SLUG_COMMIT", // Deprecated, kept for backwards compatibility
		"SOURCE_VERSION",
		"CODEBUILD_RESOLVED_SOURCE_VERSION",
		"CIRCLE_SHA1",
		"GAE_DEPLOYMENT_ID",
		"GITHUB_SHA",             // GitHub Actions - https://help.github.com/en/actions
		"COMMIT_REF",             // Netlify - https://docs.netlify.com/
		"VERCEL_GIT_COMMIT_SHA",  // Vercel - https://vercel.com/
		"ZEIT_GITHUB_COMMIT_SHA", // Zeit (now known as Vercel)
		"ZEIT_GITLAB_COMMIT_SHA",
		"ZEIT_BITBUCKET_COMMIT_SHA",
	}
	for _, e := range envs {
		if release = os.Getenv(e); release != "" {
			debuglog.Printf("Using release from environment variable %s: %s", e, release)
			return release
		}
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		buildInfoVcsRevision := revisionFromBuildInfo(info)
		if len(buildInfoVcsRevision) > 0 {
			return buildInfoVcsRevision
		}
	}

	// Derive a version string from Git. Example outputs:
	// 	v1.0.1-0-g9de4
	// 	v2.0-8-g77df-dirty
	// 	4f72d7
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "describe", "--long", "--always", "--dirty")
		b, err := cmd.Output()
		if err != nil {
			// Either Git is not available or the current directory is not a
			// Git repository.
			var s strings.Builder
			fmt.Fprintf(&s, "Release detection failed: %v", err)
			if err, ok := err.(*exec.ExitError); ok && len(err.Stderr) > 0 {
				fmt.Fprintf(&s, ": %s", err.Stderr)
			}
			debuglog.Print(s.String())
		} else {
			release = strings.TrimSpace(string(b))
			debuglog.Printf("Using release from Git: %s", release)
			return release
		}
	}

	debuglog.Print("Some Sentry features will not be available. See https://docs.sentry.io/product/releases/.")
	debuglog.Print("To stop seeing this message, pass a Release to sentry.Init or set the SENTRY_RELEASE environment variable.")
	return ""
}

func revisionFromBuildInfo(info *debug.BuildInfo) string {
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			debuglog.Printf("Using release from debug info: %s", setting.Value)
			return setting.Value
		}
	}

	return ""
}

// jsonObjectSize accumulates the serialized size of a JSON object.
type jsonObjectSize int

// field adds a "key":value member along with its separator.
func (s *jsonObjectSize) field(key string, valueSize int) {
	*s += jsonObjectSize(jsonStringSize(key) + 1 + valueSize + 1)
}

func (s jsonObjectSize) total() int {
	if s == 0 {
		return len("{}")
	}
	return int(s) + 1
}

func jsonStringSize(s string) int {
	return len(s) + 2
}

func jsonHexSize(n int) int {
	return hex.EncodedLen(n) + 2
}

func jsonBoolSize(b bool) int {
	if b {
		return len("true")
	}
	return len("false")
}

func jsonIntSize(i int64) int {
	var buf [20]byte
	return len(strconv.AppendInt(buf[:0], i, 10))
}

func jsonUintSize(u uint64) int {
	var buf [20]byte
	return len(strconv.AppendUint(buf[:0], u, 10))
}

// jsonFloatSize follows encoding/json, which uses exponent notation for very small and very large values.
func jsonFloatSize(f float64) int {
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	var buf [32]byte
	return len(strconv.AppendFloat(buf[:0], f, format, -1, 64))
}

func jsonTimeSize(t time.Time) int {
	var buf [64]byte
	return len(t.AppendFormat(buf[:0], time.RFC3339Nano)) + 2
}

func jsonArraySize[T any](elems []T, elemSize func(T) int) int {
	size := len("[]")
	for i, elem := range elems {
		if i > 0 {
			size++
		}
		size += elemSize(elem)
	}
	return size
}

func jsonAttributesSize(attrs map[string]attribute.Value) int {
	var size jsonObjectSize
	for k, v := range attrs {
		size.field(k, jsonAttributeSize(v))
	}
	return size.total()
}

// jsonAttributeSize returns the size of v serialized as {"value":...,"type":"..."}.
func jsonAttributeSize(v attribute.Value) int {
	var size jsonObjectSize
	size.field("value", jsonAttributeValueSize(v))
	size.field("type", jsonStringSize(jsonAttributeType(v.Type())))
	return size.total()
}

func jsonAttributeValueSize(v attribute.Value) int {
	switch v.Type() {
	case attribute.BOOL:
		return jsonBoolSize(v.AsBool())
	case attribute.INT64:
		return jsonIntSize(v.AsInt64())
	case attribute.UINT64:
		return jsonUintSize(v.AsUint64())
	case attribute.FLOAT64:
		return jsonFloatSize(v.AsFloat64())
	case attribute.STRING:
		return jsonStringSize(v.AsString())
	case attribute.BOOLSLICE:
		return jsonArraySize(v.AsBoolSlice(), jsonBoolSize)
	case attribute.INT64SLICE:
		return jsonArraySize(v.AsInt64Slice(), jsonIntSize)
	case attribute.FLOAT64SLICE:
		return jsonArraySize(v.AsFloat64Slice(), jsonFloatSize)
	case attribute.STRINGSLICE:
		return jsonArraySize(v.AsStringSlice(), jsonStringSize)
	default:
		return len("{}")
	}
}

func jsonAttributeType(t attribute.Type) string {
	switch t {
	case attribute.BOOL:
		return "boolean"
	case attribute.INT64, attribute.UINT64:
		return "integer"
	case attribute.FLOAT64:
		return "double"
	case attribute.STRING:
		return "string"
	case attribute.BOOLSLICE, attribute.INT64SLICE, attribute.FLOAT64SLICE, attribute.STRINGSLICE:
		return "array"
	default:
		return ""
	}
}
