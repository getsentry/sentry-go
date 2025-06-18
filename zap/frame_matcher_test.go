package sentryzap

import (
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func Test_core_filterFrames(t *testing.T) {
	t.Parallel()
	type args struct {
		frames []sentry.Frame
	}
	tests := []struct {
		name                string
		matcher             FrameMatcher
		args                args
		wantRemainingFrames int
	}{
		{
			name:    "Empty filter set - do not filter anything at all",
			matcher: FrameMatchers{},
			args: args{
				[]sentry.Frame{
					{
						Module: "github.com/getsentry/sentry-go/zapsentry",
					},
				},
			},
			wantRemainingFrames: 1,
		},
		{
			name:    "Default filter set - filter frames from zap",
			matcher: defaultFrameMatchers,
			args: args{
				[]sentry.Frame{
					{
						Module: "go.uber.org/zap",
					},
				},
			},
			wantRemainingFrames: 0,
		},
		{
			name: "Custom filter - ignore if test file",
			matcher: FrameMatcherFunc(func(f sentry.Frame) bool {
				return strings.HasSuffix(f.Filename, "_test.go")
			}),
			args: args{
				[]sentry.Frame{
					{
						Filename: "core_test.go",
					},
					{
						Filename: "core.go",
					},
				},
			},
			wantRemainingFrames: 1,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &core{
				cfg: &Configuration{
					FrameMatcher: tt.matcher,
				},
			}
			got := c.filterFrames(tt.args.frames)
			if len(got) != tt.wantRemainingFrames {
				t.Errorf("filterFrames() = %v, want %v", got, tt.wantRemainingFrames)
			}
		})
	}
}
