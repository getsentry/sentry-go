package debuglog

import (
	"bytes"
	"io"
	stdlog "log"
	"strings"
	"sync"
	"testing"
)

func TestSetLogger(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	var buf bytes.Buffer
	newLogger := stdlog.New(&buf, "[Test] ", stdlog.LstdFlags)
	SetLogger(newLogger)

	if GetLogger() != newLogger {
		t.Error("SetLogger did not set the logger correctly")
	}
}

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	if logger == nil {
		t.Error("GetLogger returned nil")
	}
}

func TestPrintf(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	var buf bytes.Buffer
	testLogger := stdlog.New(&buf, "", 0)
	SetLogger(testLogger)
	Printf("test %s %d", "message", 42)

	output := buf.String()
	if !strings.Contains(output, "test message 42") {
		t.Errorf("Printf output incorrect: got %q", output)
	}
}

func TestPrintln(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	var buf bytes.Buffer
	testLogger := stdlog.New(&buf, "", 0)
	SetLogger(testLogger)
	Println("test", "message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Println output incorrect: got %q", output)
	}
}

func TestPrint(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	var buf bytes.Buffer
	testLogger := stdlog.New(&buf, "", 0)
	SetLogger(testLogger)
	Print("test", "message")

	output := buf.String()
	if !strings.Contains(output, "testmessage") {
		t.Errorf("Print output incorrect: got %q", output)
	}
}

func TestConcurrentAccess(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	var wg sync.WaitGroup
	iterations := 100

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
			newLogger := stdlog.New(io.Discard, "[Test] ", stdlog.LstdFlags)
			SetLogger(newLogger)
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
	testLogger := stdlog.New(&buf, "", 0)
	SetLogger(testLogger)
	Printf("test")
	Println("test")
	Print("test")
}

func TestNilLogger(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	SetLogger(nil)
	Printf("test")
	Println("test")
	Print("test")
}
