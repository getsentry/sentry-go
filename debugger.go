package sentry

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

var debugger = NewDebugger()

const prefix = "[Sentry]"

type Debugger struct {
	writer       io.Writer
	customWriter io.Writer
}

func NewDebugger() *Debugger {
	return &Debugger{
		writer: ioutil.Discard,
	}
}
func (debugger *Debugger) Enable() {
	if debugger.customWriter != nil {
		debugger.writer = debugger.customWriter
	} else {
		debugger.writer = os.Stdout
	}
}

func (debugger *Debugger) Disable() {
	debugger.writer = ioutil.Discard
}

func (debugger *Debugger) SetOutput(writer io.Writer) {
	debugger.writer = writer
	debugger.customWriter = writer
}

// TODO: This shoooould probably be reworked?
func (debugger *Debugger) Print(v ...interface{}) {
	fmt.Fprint(debugger.writer, append([]interface{}{prefix + " "}, v...)...)
}

func (debugger *Debugger) Println(v ...interface{}) {
	fmt.Fprintln(debugger.writer, append([]interface{}{prefix}, v...)...)
}

func (debugger *Debugger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(debugger.writer, prefix+" "+format, v...)
}
