package sentry

import (
	"runtime"
	"strconv"
	"time"
)

// Start collecting profile data and returns a function that stops profiling, producing a Trace.
func startProfiling() func() *profileTrace {
	trace := &profileTrace{
		Frames:         make([]Frame, 0, 100),
		Samples:        make([]profileSample, 0, 100),
		Stacks:         make([]profileStack, 0, 100),
		ThreadMetadata: make(map[string]profileThreadMetadata, 10),
	}
	profiler := &profileRecorder{
		startTime:     time.Now(),
		trace:         trace,
		recordsBuffer: make([]runtime.StackRecord, runtime.NumGoroutine()+stacksBufferGrow),
		stackIndexes:  make(map[[32]uintptr]int, cap(trace.Stacks)),
		frameIndexes:  make(map[uintptr]int, cap(trace.Frames)),
	}
	signal := make(chan struct{})

	// Periodically collect stacks.
	ticker := time.NewTicker(time.Second / 101) // 101 Hz

	// Stop after 30 seconds unless stopped manually.
	timeout := time.AfterFunc(30*time.Second, func() { signal <- struct{}{} })

	go func() {
		defer ticker.Stop()
		defer timeout.Stop()

		for {
			select {
			case <-ticker.C:
				profiler.Collect()
			case <-signal:
				return
			}
		}
	}()

	return func() *profileTrace {
		signal <- struct{}{}
		return profiler.trace
	}
}

// We keep a buffer for stack capture. This is the number by which we increase the buffer if needed.
const stacksBufferGrow = 10

// TODO we may be able to cache previously resolved frames, stacks, etc.
type profileRecorder struct {
	startTime     time.Time
	trace         *profileTrace
	recordsBuffer []runtime.StackRecord

	// Map from runtime.StackRecord.Stack0 to an index trace.Stacks
	stackIndexes map[[32]uintptr]int

	// Map from runtime.Frame.PC to an index trace.Frames
	frameIndexes map[uintptr]int
}

func (p *profileRecorder) Collect() {
	for {
		// Capture stacks for all existing goroutines.
		if n, ok := runtime.GoroutineProfile(p.recordsBuffer); ok {
			p.processRecords(p.recordsBuffer[0:n])
			break
		} else {
			// In case the buffer was too small, we grow it and try again.
			p.recordsBuffer = make([]runtime.StackRecord, n+stacksBufferGrow)
		}
	}
}

func (p *profileRecorder) processRecords(records []runtime.StackRecord) {
	elapsedNs := uint64(time.Since(p.startTime).Nanoseconds())
	for gid, record := range records {
		stackIndex := p.addStackTrace(record)
		if stackIndex < 0 {
			return
		}

		// TODO we can't get any useful Goroutine identification, see https://github.com/golang/go/issues/59663
		//      this is just for testing purposes - we need to switch to pprof.
		threadIndex := p.addThread(gid)

		p.trace.Samples = append(p.trace.Samples, profileSample{
			ElapsedSinceStartNS: elapsedNs,
			StackID:             stackIndex,
			ThreadID:            threadIndex,
		})
	}
}

func (p *profileRecorder) addStackTrace(record runtime.StackRecord) int {
	index, exists := p.stackIndexes[record.Stack0]

	if !exists {
		runtimeFrames := extractFrames(record.Stack())
		stack := make(profileStack, 0, len(runtimeFrames))
		for _, frame := range runtimeFrames {
			if frameIndex := p.addFrame(frame); frameIndex >= 0 {
				stack = append(stack, frameIndex)
			}
		}
		index = len(p.trace.Stacks)
		p.trace.Stacks = append(p.trace.Stacks, stack)
		p.stackIndexes[record.Stack0] = index
	}

	return index
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

func (p *profileRecorder) addFrame(frame runtime.Frame) int {
	index, exists := p.frameIndexes[frame.PC]
	if !exists {
		index = len(p.trace.Frames)
		p.trace.Frames = append(p.trace.Frames, NewFrame(frame))
		p.frameIndexes[frame.PC] = index
	}
	return index
}
