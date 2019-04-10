package sentry

import (
	"io/ioutil"
	"os"
	"testing"
)

type customWriter struct {
	lastMessage string
}

func (w *customWriter) Write(p []byte) (n int, err error) {
	w.lastMessage = string(p)
	return
}

func TestNewDebugger(t *testing.T) {
	debugger := NewDebugger()

	if ioutil.Discard != debugger.writer {
		t.Error("expected ioutil.Discard to be default writer")
	}

	if debugger.customWriter != nil {
		t.Error("expected customWriter to be nil by default")
	}
}

func TestSetOutput(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}

	debugger.SetOutput(customWriter)

	if customWriter != debugger.writer {
		t.Error("expected customWriter to be set as the new writer")
	}

	if customWriter != debugger.customWriter {
		t.Error("expected customWriter to be cached as customWriter")
	}
}

func TestEnableUsesDefaultWriter(t *testing.T) {
	debugger := NewDebugger()

	debugger.Enable()

	if os.Stdout != debugger.writer {
		t.Error("expected os.Stdout to be default writer after enabling")
	}
}

func TestEnableUsesConfiguredOutputWhenAvailable(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}

	debugger.SetOutput(customWriter)
	debugger.Enable()

	if customWriter != debugger.writer {
		t.Error("expected customWriter to be used after enabling")
	}
}

func TestEnableRemembersConfiguredOutput(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}

	debugger.SetOutput(customWriter)
	debugger.Disable()
	debugger.Enable()

	if customWriter != debugger.writer {
		t.Error("expected customWriter to be used after disabling and re-enabling")
	}
}

func TestDisableDiscardsEverything(t *testing.T) {
	debugger := NewDebugger()

	debugger.Disable()

	if ioutil.Discard != debugger.writer {
		t.Error("expected disabled debugger to use ioutil.Discard as the writer")
	}
}

func TestPrintWritesToConfiguredWriterAndAppendsPrefix(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}
	debugger.SetOutput(customWriter)
	debugger.Print("random", "message")
	assertEqual(t, customWriter.lastMessage, "[Sentry] randommessage")
}

func TestPrintlnWritesToConfiguredWriterAndAppendsPrefix(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}
	debugger.SetOutput(customWriter)
	debugger.Println("random", "message")
	assertEqual(t, customWriter.lastMessage, "[Sentry] random message\n")
}

func TestPrintfWritesToConfiguredWriterAndAppendsPrefix(t *testing.T) {
	debugger := NewDebugger()
	customWriter := &customWriter{}
	debugger.SetOutput(customWriter)
	debugger.Printf("%s %d", "random", 42)
	assertEqual(t, customWriter.lastMessage, "[Sentry] random 42")
}
