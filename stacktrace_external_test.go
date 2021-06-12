package sentry_test

import (
	"path/filepath"
	"testing"

	goErrors "github.com/go-errors/errors"
	pingcapErrors "github.com/pingcap/errors"
	pkgErrors "github.com/pkg/errors"
	"golang.org/x/xerrors"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/getsentry/sentry-go"
)

func f1() *sentry.Stacktrace {
	return sentry.NewStacktrace()
}

func f2() *sentry.Stacktrace {
	return f1()
}
func f3() *sentry.Stacktrace {
	return sentry.NewStacktraceForTest()
}

func RedPkgErrorsRanger() error {
	return BluePkgErrorsRanger()
}

func BluePkgErrorsRanger() error {
	return pkgErrors.New("this is bad from pkgErrors")
}

func RedPingcapErrorsRanger() error {
	return BluePingcapErrorsRanger()
}

func BluePingcapErrorsRanger() error {
	return pingcapErrors.New("this is bad from pingcapErrors")
}

func RedGoErrorsRanger() error {
	return BlueGoErrorsRanger()
}

func BlueGoErrorsRanger() error {
	return goErrors.New("this is bad from goErrors")
}

func RedXErrorsRanger() error {
	err := BlueXErrorsRanger()
	return xerrors.Errorf("context in RedXErrorsRanger: %w", err)
}

func BlueXErrorsRanger() error {
	return xerrors.New("this is bad from xerrors")
}

//nolint: scopelint // false positive https://github.com/kyoh86/scopelint/issues/4
func TestNewStacktrace(t *testing.T) {
	tests := map[string]struct {
		f    func() *sentry.Stacktrace
		want *sentry.Stacktrace
	}{
		"f1": {f1, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "f1",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   19,
					InApp:    true,
				},
			},
		}},
		"f2": {f2, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "f2",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   23,
					InApp:    true,
				},
				{
					Function: "f1",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   19,
					InApp:    true,
				},
			},
		}},
		// test that functions in the SDK that call NewStacktrace are not part
		// of the resulting Stacktrace.
		"NewStacktraceForTest": {sentry.NewStacktraceForTest, &sentry.Stacktrace{
			Frames: []sentry.Frame{},
		}},
		"f3": {f3, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "f3",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   26,
					InApp:    true,
				},
			},
		}},
		"StacktraceTestHelper": {sentry.StacktraceTestHelper{}.NewStacktrace, &sentry.Stacktrace{
			Frames: []sentry.Frame{},
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.f()
			compareStacktrace(t, true, got, tt.want)
		})
	}
}

//nolint: scopelint // false positive https://github.com/kyoh86/scopelint/issues/4
func TestExtractStacktrace(t *testing.T) {
	tests := map[string]struct {
		f    func() error
		skip bool
		want *sentry.Stacktrace
	}{
		// https://github.com/pkg/errors
		"pkg/errors": {RedPkgErrorsRanger, true, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "RedPkgErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   30,
					InApp:    true,
				},
				{
					Function: "BluePkgErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   34,
					InApp:    true,
				},
			},
		}},
		// https://github.com/pingcap/errors
		"pingcap/errors": {RedPingcapErrorsRanger, true, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "RedPingcapErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   38,
					InApp:    true,
				},
				{
					Function: "BluePingcapErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   42,
					InApp:    true,
				},
			},
		}},
		// https://github.com/go-errors/errors
		"go-errors/errors": {RedGoErrorsRanger, true, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "RedGoErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   46,
					InApp:    true,
				},
				{
					Function: "BlueGoErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   50,
					InApp:    true,
				},
			},
		}},
		// https://golang.org/x/xerrors
		"x/errors": {RedXErrorsRanger, false, &sentry.Stacktrace{
			Frames: []sentry.Frame{
				{
					Function: "RedXErrorsRanger",
					Module:   "github.com/getsentry/sentry-go_test",
					Lineno:   55,
					InApp:    true,
				},
			},
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.f()
			if err == nil {
				t.Fatal("got nil error")
			}
			got := sentry.ExtractStacktrace(err)
			compareStacktrace(t, tt.skip, got, tt.want)
			// We ignore paths in compareStacktrace because they depend on the
			// environment where tests are run. However, Frame.Filename should
			// be a relative path and Frame.AbsPath should be an absolute path.
			for _, frame := range got.Frames {
				if !filepath.IsAbs(frame.AbsPath) {
					t.Errorf("got %q, want absolute path", frame.AbsPath)
				}
				if filepath.IsAbs(frame.Filename) {
					t.Errorf("got %q, want relative path", frame.Filename)
				}
			}
		})
	}
}

func compareStacktrace(t *testing.T, skip bool, got, want *sentry.Stacktrace) {
	t.Helper()

	if got == nil {
		t.Fatal("got nil stack trace")
	}

	if len(got.Frames) == 0 {
		t.Fatal("got no frames")
	}

	if skip {
		// Skip anonymous function passed to t.Run.
		got.Frames = got.Frames[1:]
	}

	if diff := stacktraceDiff(want, got); diff != "" {
		t.Fatalf("Stacktrace mismatch (-want +got):\n%s", diff)
	}

	// Because stacktraceDiff ignores Frame.AbsPath, sanity check that the
	// values we got are actually absolute paths pointing to this test file.
	for _, frame := range got.Frames {
		if !filepath.IsAbs(frame.AbsPath) {
			t.Errorf("Frame{Function: %q}.AbsPath = %q, want absolute path", frame.Function, frame.AbsPath)
		}
		if filepath.Base(frame.AbsPath) != "stacktrace_external_test.go" {
			t.Errorf(`Frame{Function: %q}.AbsPath = %q, want ".../stacktrace_external_test.go"`, frame.Function, frame.AbsPath)
		}
	}
}

func stacktraceDiff(x, y *sentry.Stacktrace) string {
	return cmp.Diff(
		x, y,
		cmpopts.IgnoreFields(sentry.Frame{}, "AbsPath", "Filename"),
	)
}
