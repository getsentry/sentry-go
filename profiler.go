package sentry

import (
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go/internal/traceparser"
)

// Start collecting profile data and returns a function that stops profiling, producing a Trace.
// May return nil or an incomplete trace in case of a panic.
func startProfiling() func() *profileTrace {
	// buffered channels to handle the recover() case without blocking
	result := make(chan *profileTrace, 2)
	stopSignal := make(chan struct{}, 2)

	go profilerGoroutine(result, stopSignal)

	return func() *profileTrace {
		stopSignal <- struct{}{}
		return <-result
	}
}

// This allows us to test whether panic during profiling are handled correctly and don't block execution.
var testProfilerPanic = 0

func profilerGoroutine(result chan<- *profileTrace, stopSignal chan struct{}) {
	// We shouldn't panic but let's be super safe.
	defer func() {
		recover()
		// Make sure we don't block the caller of stopFn() even if we panic.
		result <- nil
	}()

	// Stop after 30 seconds unless stopped manually.
	timeout := time.AfterFunc(30*time.Second, func() { stopSignal <- struct{}{} })
	defer timeout.Stop()

	if testProfilerPanic == 1 {
		panic("This is an expected panic in profilerGoroutine() during tests")
	}

	// Periodically collect stacks.
	collectTicker := time.NewTicker(profilerSamplingRate)
	defer collectTicker.Stop()

	profiler := newProfiler()

	defer func() {
		result <- profiler.trace
	}()

	for {
		select {
		case <-collectTicker.C:
			profiler.OnTick()
		case <-stopSignal:
			return
		}
	}
}

func newProfiler() *profileRecorder {
	trace := &profileTrace{
		Frames:         make([]*Frame, 0, 20),
		Samples:        make([]*profileSample, 0, 100),
		Stacks:         make([]profileStack, 0, 10),
		ThreadMetadata: make(map[string]profileThreadMetadata, 10),
	}

	return &profileRecorder{
		startTime:    time.Now(),
		trace:        trace,
		stackIndexes: make(map[string]int, cap(trace.Stacks)),
		frameIndexes: make(map[string]int, cap(trace.Frames)),
		stacksBuffer: make([]byte, 32*1024),
	}
}

const profilerSamplingRate = time.Second / 101 // 101 Hz
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

func (p *profileRecorder) OnTick() {
	elapsedNs := uint64(time.Since(p.startTime).Nanoseconds())

	records := p.collectRecords()
	p.processRecords(elapsedNs, records)

	// Free up some memory if we don't need such a large buffer anymore.
	if len(p.stacksBuffer) > len(records)*3 {
		p.stacksBuffer = make([]byte, len(records)*3)
	}

	if testProfilerPanic == 2 && elapsedNs > 10_000_000 {
		panic("This is an expected panic in Profiler.OnTick() during tests")
	}
}

func (p *profileRecorder) collectRecords() []byte {
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
			return p.stacksBuffer[0:n]
		}
	}
}

func (p *profileRecorder) processRecords(elapsedNs uint64, stacksBuffer []byte) {
	var stacks = traceparser.Parse(stacksBuffer)
	for i := 0; i < stacks.Length; i++ {
		var stack = stacks.Item(i)
		threadIndex := p.addThread(int(stack.GoID()))
		stackIndex := p.addStackTrace(stack)
		if stackIndex < 0 {
			return
		}

		p.trace.Samples = append(p.trace.Samples, &profileSample{
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
