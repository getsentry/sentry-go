package sentry

import (
	"time"

	"github.com/google/uuid"
)

const Version string = "0.0.0-beta"
const UserAgent string = "sentry.go/" + Version

type ClientSdkInfo struct {
	name    string
	version string
}

type Client struct {
	environment string
}

func NewClient() *Client {
	return &Client{}
}

type LogEntry struct {
	message string
	params  []interface{}
}

type Request struct{}

type Exception struct{}

type Context string

type Event struct {
	eventID     uuid.UUID
	level       Level // TODO: Set LevelInfo as the default
	fingerprint []string
	culprit     string
	transaction string
	message     string
	logentry    LogEntry
	logger      string
	modules     map[string]string
	platform    string
	timestamp   time.Time
	serverName  string
	release     string
	dist        string
	environment string
	user        User
	request     Request
	contexts    map[string]Context
	breadcrumbs []*Breadcrumb
	exception   []Exception
	tags        map[string]string
	extra       map[string]interface{}
	sdk         ClientSdkInfo
}
