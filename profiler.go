package sentry

import (
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go/internal/traceparser"
)

// Start collecting profile data and returns a function that stops profiling, producing a Trace.
func startProfiling() func() *profileTrace {
	result := make(chan *profileTrace)
	stopSignal := make(chan struct{})

	// Stop after 30 seconds unless stopped manually.
	timeout := time.AfterFunc(30*time.Second, func() { stopSignal <- struct{}{} })

	// Periodically collect stacks.
	collectTicker := time.NewTicker(time.Second / 101) // 101 Hz

	go func() {
		defer collectTicker.Stop()
		defer timeout.Stop()

		trace := &profileTrace{
			Frames:         make([]*Frame, 0, 20),
			Samples:        make([]profileSample, 0, 100),
			Stacks:         make([]profileStack, 0, 10),
			ThreadMetadata: make(map[string]profileThreadMetadata, 10),
		}
		profiler := &profileRecorder{
			startTime:    time.Now(),
			trace:        trace,
			stackIndexes: make(map[string]int, cap(trace.Stacks)),
			frameIndexes: make(map[string]int, cap(trace.Frames)),
			stacksBuffer: make([]byte, 32*1024),
		}

		defer func() {
			result <- trace
		}()

		for {
			select {
			case <-collectTicker.C:
				profiler.Collect()
			case <-stopSignal:
				return
			}
		}
	}()

	return func() *profileTrace {
		stopSignal <- struct{}{}
		return <-result
	}
}

const stackBufferMaxGrowth = 512 * 1024
const stackBufferLimit = 10 * 1024 * 1024

// TODO we are be able to cache previously resolved frames, stacks, readBuffer, etc.
type profileRecorder struct {
	startTime time.Time
	trace     *profileTrace

	// Buffer to read current stacks - will grow automatically up to stackBufferLimit.
	stacksBuffer []byte

	// Map from runtime.StackRecord.Stack0 to an index trace.Stacks
	stackIndexes map[string]int

	// Map from runtime.Frame.PC to an index trace.Frames
	frameIndexes map[string]int
}

func (p *profileRecorder) Collect() {
	for {
		// Capture stacks for all existing goroutines.
		// Note: runtime.GoroutineProfile() would be better but we can't use it at the moment because
		//       it doesn't give us `gid` for each routine, see https://github.com/golang/go/issues/59663
		n := runtime.Stack(p.stacksBuffer, true)

		// If we couldn't read everything, increase the buffer and try again.
		if n >= len(p.stacksBuffer) && n < stackBufferLimit {
			var newSize = n * 2
			if newSize > n+stackBufferMaxGrowth {
				newSize = n + stackBufferMaxGrowth
			}
			if newSize > stackBufferLimit {
				newSize = stackBufferLimit
			}
			p.stacksBuffer = make([]byte, newSize)

		} else {
			p.processRecords(p.stacksBuffer[0:n])

			// Free up some memory if we don't need such a large buffer anymore.
			if len(p.stacksBuffer) > n*3 {
				p.stacksBuffer = make([]byte, n*3)
			}

			break
		}
	}
}

func (p *profileRecorder) processRecords(stacksBuffer []byte) {
	elapsedNs := uint64(time.Since(p.startTime).Nanoseconds())
	var stacks = traceparser.Parse(stacksBuffer)
	for i := 0; i < stacks.Length; i++ {
		var stack = stacks.Item(i)
		threadIndex := p.addThread(int(stack.GoID()))
		stackIndex := p.addStackTrace(stack)
		if stackIndex < 0 {
			return
		}

		p.trace.Samples = append(p.trace.Samples, profileSample{
			ElapsedSinceStartNS: elapsedNs,
			StackID:             stackIndex,
			ThreadID:            threadIndex,
		})
	}
}

func (p *profileRecorder) addThread(id int) uint64 {
	index := strconv.Itoa(id)
	if _, exists := p.trace.ThreadMetadata[index]; !exists {
		p.trace.ThreadMetadata[index] = profileThreadMetadata{
			Name: "Goroutine " + index,
		}
	}
	return uint64(id)
}

func (p *profileRecorder) addStackTrace(capturedStack traceparser.Trace) int {
	// NOTE: Don't convert to string yet, it's expensive and compiler can avoid it when
	//       indexing into a map (only needs a copy when adding a new key to the map).
	var key = capturedStack.UniqueIdentifier()

	stackIndex, exists := p.stackIndexes[string(key)]
	if !exists {
		iter := capturedStack.FramesReversed()
		stack := make(profileStack, 0, iter.LengthUpperBound())
		for iter.HasNext() {
			var frame = iter.Next()

			if frameIndex := p.addFrame(frame); frameIndex >= 0 {
				stack = append(stack, frameIndex)
			}
		}
		stackIndex = len(p.trace.Stacks)
		p.trace.Stacks = append(p.trace.Stacks, stack)
		p.stackIndexes[string(key)] = stackIndex
	}

	return stackIndex
}

func (p *profileRecorder) addFrame(capturedFrame traceparser.Frame) int {
	// NOTE: Don't convert to string yet, it's expensive and compiler can avoid it when
	//       indexing into a map (only needs a copy when adding a new key to the map).
	var key = capturedFrame.UniqueIdentifier()

	frameIndex, exists := p.frameIndexes[string(key)]
	if !exists {
		module, function := splitQualifiedFunctionName(string(capturedFrame.Func()))
		file, line := capturedFrame.File()
		frame := &Frame{
			Lineno:   line,
			Module:   module,
			Function: function,
		}

		path := string(file)
		if filepath.IsAbs(path) {
			frame.AbsPath = path
		} else {
			frame.Filename = path
		}

		setInAppFrame(frame)

		frameIndex = len(p.trace.Frames)
		p.trace.Frames = append(p.trace.Frames, frame)
		p.frameIndexes[string(key)] = frameIndex
	}
	return frameIndex
}
