package sentry

type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
	LevelFatal   Level = "fatal"
)

const (
	SdkName      string = "sentry.go"
	SdkVersion   string = "0.0.0-beta"
	SdkUserAgent string = SdkName + "/" + SdkVersion
)

// https://docs.sentry.io/development/sdk-dev/interfaces/sdk/
type ClientSdkInfo struct {
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	Packages []ClientSdkPackage `json:"packages"`
}

type ClientSdkPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TODO: This type could be more useful, as map of interface{} is too generic
// and requires a lot of type assertions in beforeBreadcrumb calls
// plus it could just be `map[string]interface{}` then
type BreadcrumbHint map[string]interface{}

// https://docs.sentry.io/development/sdk-dev/interfaces/breadcrumbs/
type Breadcrumb struct {
	Category  string                 `json:"category"`
	Data      map[string]interface{} `json:"data"`
	Level     Level                  `json:"level"`
	Message   string                 `json:"message"`
	Timestamp int64                  `json:"timestamp"`
	Type      string                 `json:"type"`
}

// https://docs.sentry.io/development/sdk-dev/interfaces/user/
type User struct {
	Email     string `json:"email"`
	ID        string `json:"id"`
	IPAddress string `json:"ip_address"`
	Username  string `json:"username"`
}

// https://docs.sentry.io/development/sdk-dev/interfaces/http/
type Request struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Data        string            `json:"data"`
	QueryString string            `json:"query_string"`
	Cookies     string            `json:"cookies"`
	Headers     map[string]string `json:"headers"`
	Env         map[string]string `json:"env"`
}

// https://docs.sentry.io/development/sdk-dev/interfaces/stacktrace/
type Frame struct {
	Function    string                 `json:"function"`
	Symbol      string                 `json:"symbol"`
	Module      string                 `json:"module"`
	Package     string                 `json:"package"`
	Filename    string                 `json:"filename"`
	AbsPath     string                 `json:"abs_path"`
	Lineno      uint                   `json:"lineno"`
	Colno       uint                   `json:"colno"`
	PreContext  []string               `json:"pre_context"`
	ContextLine string                 `json:"context_line"`
	PostContext []string               `json:"post_context"`
	InApp       bool                   `json:"in_app"`
	Vars        map[string]interface{} `json:"vars"`
}

type Stacktrace struct {
	Frames        []Frame `json:"frames"`
	FramesOmitted [2]uint `json:"frames_omitted"`
}

// https://docs.sentry.io/development/sdk-dev/interfaces/exception/
type Exception struct {
	Type          string     `json:"type"`
	Value         string     `json:"value"`
	Module        string     `json:"module"`
	Stacktrace    Stacktrace `json:"stacktrace"`
	RawStacktrace Stacktrace `json:"raw_stacktrace"`
}

// https://docs.sentry.io/development/sdk-dev/attributes/
type Event struct {
	Breadcrumbs []*Breadcrumb          `json:"breadcrumbs"`
	Dist        string                 `json:"dist"`
	Environment string                 `json:"environment"`
	EventID     string                 `json:"event_id"`
	Extra       map[string]interface{} `json:"extra"`
	Fingerprint []string               `json:"fingerprint"`
	Level       Level                  `json:"level"`
	Message     string                 `json:"message"`
	Platform    string                 `json:"platform"`
	Release     string                 `json:"release"`
	Sdk         ClientSdkInfo          `json:"sdk"`
	ServerName  string                 `json:"server_name"`
	Tags        map[string]string      `json:"tags"`
	Timestamp   int64                  `json:"timestamp"`
	Transaction string                 `json:"transaction"`
	User        User                   `json:"user"`
	Logger      string                 `json:"logger"`
	Modules     map[string]string      `json:"modules"`
	Request     Request                `json:"request"`
	Exception   []Exception            `json:"exception"`
}

type EventHint struct {
	Data              interface{}
	EventID           string
	OriginalException error
}
