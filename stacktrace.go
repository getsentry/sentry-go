package sentry

import (
	"go/build"
	"path/filepath"
	"runtime"
	"strings"
)

const unknown string = "unknown"

// // The module download is split into two parts: downloading the go.mod and downloading the actual code.
// // If you have dependencies only needed for tests, then they will show up in your go.mod,
// // and go get will download their go.mods, but it will not download their code.
// // The test-only dependencies get downloaded only when you need it, such as the first time you run go test.
// //
// // https://github.com/golang/go/issues/26913#issuecomment-411976222

type Stacktrace struct {
	Frames        []Frame `json:"frames"`
	FramesOmitted [2]uint `json:"frames_omitted"`
}

func NewStacktrace() *Stacktrace {
	callerPcs := make([]uintptr, 100)
	callersCount := runtime.Callers(0, callerPcs)

	if callersCount == 0 {
		return nil
	}

	stacktrace := Stacktrace{
		Frames: extractFrames(callerPcs),
	}

	return &stacktrace
}

// https://docs.sentry.io/development/sdk-dev/interfaces/stacktrace/
type Frame struct {
	Function    string                 `json:"function"`
	Symbol      string                 `json:"symbol"`
	Module      string                 `json:"module"`
	Package     string                 `json:"package"`
	Filename    string                 `json:"filename"`
	AbsPath     string                 `json:"abs_path"`
	Lineno      int                    `json:"lineno"`
	Colno       int                    `json:"colno"`
	PreContext  []string               `json:"pre_context"`
	ContextLine string                 `json:"context_line"`
	PostContext []string               `json:"post_context"`
	InApp       bool                   `json:"in_app"`
	Vars        map[string]interface{} `json:"vars"`
}

func NewFrame(pc uintptr, fName, file string, line int) Frame {
	if file == "" {
		file = unknown
	}

	if fName == "" {
		fName = unknown
	}

	frame := Frame{
		AbsPath:  file,
		Filename: extractFilenameFromPath(file),
		Lineno:   line,
	}
	frame.Module, frame.Function = deconstructFunctionName(fName)
	frame.InApp = isInAppFrame(frame)

	return frame
}

func extractFrames(pcs []uintptr) []Frame {
	var frames []Frame
	callersFrames := runtime.CallersFrames(pcs)

	for {
		callerFrame, more := callersFrames.Next()
		frame := NewFrame(callerFrame.PC, callerFrame.Function, callerFrame.File, callerFrame.Line)
		frames = append([]Frame{frame}, frames...)

		if !more {
			break
		}
	}

	return frames
}

var _cachedPossiblePaths []string

func possiblePaths() []string {
	if _cachedPossiblePaths != nil {
		return _cachedPossiblePaths
	}

	srcDirs := build.Default.SrcDirs()
	paths := make([]string, len(srcDirs))
	for _, path := range srcDirs {
		if path[len(path)-1] != filepath.Separator {
			path += string(filepath.Separator)
		}
		paths = append(paths, path)
	}

	_cachedPossiblePaths = paths

	return paths
}

func extractFilenameFromPath(filename string) string {
	for _, path := range possiblePaths() {
		if trimmed := strings.TrimPrefix(filename, path); len(trimmed) < len(filename) {
			return trimmed
		}
	}
	return filename
}

func isInAppFrame(frame Frame) bool {
	if frame.Module == "main" {
		return true
	}

	if !strings.Contains(frame.Module, "vendor") && !strings.Contains(frame.Module, "third_party") {
		return true
	}

	return false
}

// Transform `runtime/debug.*T·ptrmethod` into `{ pack: runtime/debug, name: *T.ptrmethod }`
func deconstructFunctionName(name string) (string, string) {
	var pack string
	if idx := strings.LastIndex(name, "."); idx != -1 {
		pack = name[:idx]
		name = name[idx+1:]
	}
	name = strings.Replace(name, "·", ".", -1)
	return pack, name
}
