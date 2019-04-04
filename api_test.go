package sentry

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type APISuite struct {
	suite.Suite
}

func TestAPISuite(t *testing.T) {
	suite.Run(t, new(APISuite))
}

// func (suite *APISuite) SetupTest() {

// }

// type FakeHub struct{}

// func (hub *FakeHub) CaptureEvent(even *Event) {

// }

// func FakeGetCurrentHub() (*Hub, error) {
// 	return &FakeHub{}, nil
// }

// func (suite *APISuite) TestCaptureEvent() {
// 	getCurrentHub = FakeGetCurrentHub

// 	event := &Event{}
// 	CaptureEvent(event)
// 	suite.True(false)
// }
