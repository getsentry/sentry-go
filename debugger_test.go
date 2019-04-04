package sentry

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
)

var lastMessage string

type CustomWriter struct {
	lastMessage string
}

func (w *CustomWriter) Write(p []byte) (n int, err error) {
	lastMessage = string(p)
	return 0, nil
}

type DebuggerSuite struct {
	suite.Suite
	debugger     *Debugger
	customWriter io.Writer
	lastMessage  string
}

func (suite *DebuggerSuite) SetupTest() {
	suite.debugger = NewDebugger()
	suite.customWriter = &CustomWriter{}
}

func TestDebuggerSuite(t *testing.T) {
	suite.Run(t, new(DebuggerSuite))
}

func (suite *DebuggerSuite) TestNewDebugger() {
	suite.Equal(ioutil.Discard, suite.debugger.writer)
	suite.Nil(suite.debugger.customWriter)
}

func (suite *DebuggerSuite) TestSetOutput() {
	suite.debugger.SetOutput(suite.customWriter)

	suite.Equal(suite.customWriter, suite.debugger.writer)
	suite.Equal(suite.customWriter, suite.debugger.customWriter)
}

func (suite *DebuggerSuite) TestEnableUsesDefaultWriter() {
	suite.debugger.Enable()

	suite.Equal(os.Stdout, suite.debugger.writer)
}

func (suite *DebuggerSuite) TestEnableUsesConfiguredOutputWhenAvailable() {
	suite.debugger.SetOutput(suite.customWriter)
	suite.debugger.Enable()

	suite.Equal(suite.customWriter, suite.debugger.writer)
}

func (suite *DebuggerSuite) TestEnableRemembersConfiguredOutput() {
	suite.debugger.SetOutput(suite.customWriter)

	suite.debugger.Disable()
	suite.debugger.Enable()

	suite.Equal(suite.customWriter, suite.debugger.writer)
}

func (suite *DebuggerSuite) TestDisableDiscardsEverything() {
	suite.debugger.Disable()

	suite.Equal(ioutil.Discard, suite.debugger.writer)
}

func (suite *DebuggerSuite) TestPrintWritesToConfiguredWriterAndAppendsPrefix() {
	suite.debugger.SetOutput(suite.customWriter)
	suite.debugger.Print("random", "message")

	suite.Equal("[Sentry] randommessage", lastMessage)
}

func (suite *DebuggerSuite) TestPrintlnWritesToConfiguredWriterAndAppendsPrefix() {
	suite.debugger.SetOutput(suite.customWriter)
	suite.debugger.Println("random", "message")

	suite.Equal("[Sentry] random message\n", lastMessage)
}

func (suite *DebuggerSuite) TestPrintfWritesToConfiguredWriterAndAppendsPrefix() {
	suite.debugger.SetOutput(suite.customWriter)
	suite.debugger.Printf("%s %d", "random", 2)

	suite.Equal("[Sentry] random 2", lastMessage)
}
