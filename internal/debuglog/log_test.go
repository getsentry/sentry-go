package debuglog

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	if logger == nil {
		t.Error("GetLogger returned nil")
	}
}

func TestSetOutput(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(io.Discard)

	Printf("test %s %d", "message", 42)

	output := buf.String()
	if !strings.Contains(output, "test message 42") {
		t.Errorf("Printf output incorrect: got %q", output)
	}
}

func TestPrintf(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(io.Discard)

	Printf("test %s %d", "message", 42)

	output := buf.String()
	if !strings.Contains(output, "test message 42") {
		t.Errorf("Printf output incorrect: got %q", output)
	}
}

func TestPrintln(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(io.Discard)

	Println("test", "message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Println output incorrect: got %q", output)
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(io.Discard)

	Print("test", "message")

	output := buf.String()
	if !strings.Contains(output, "testmessage") {
		t.Errorf("Print output incorrect: got %q", output)
	}
}

func TestConcurrentAccess(_ *testing.T) {
	var wg sync.WaitGroup
	iterations := 1000

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Printf("concurrent message %d", n)
		}(i)
	}

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = GetLogger()
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetOutput(io.Discard)
		}()
	}

	wg.Wait()
}

func TestInitialization(t *testing.T) {
	// The logger should be initialized on package load
	logger := GetLogger()
	if logger == nil {
		t.Error("Logger was not initialized")
	}

	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(io.Discard)

	Printf("test")
	Println("test")
	Print("test")

	output := buf.String()
	if !strings.Contains(output, "test") {
		t.Errorf("Expected output to contain 'test', got %q", output)
	}
}
