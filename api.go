package sentry

type NoHubError struct{}

func (e *NoHubError) Error() string {
	return "No Hub Available"
}

// TODO Implement GetCurrentHub
func GetCurrentHub() (*Hub, error) {
	client := NewClient()
	scope := &Scope{}
	return NewHub(client, scope), nil
}

func CaptureEvent(event Event) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.CaptureEvent(event)
}

func CaptureMessage(message string) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.CaptureMessage(message)
}

func CaptureException(exception error) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.CaptureException(exception)
}

func AddBreadcrumb(breadcrumb Breadcrumb) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.AddBreadcrumb(breadcrumb)
}

func WithScope(f func()) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.WithScope(f)
}

func ConfigureScope(f func(scope *Scope)) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.ConfigureScope(f)
}

func PushScope() {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.PushScope()
}
func PopScope() {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.PopScope()
}

func Flush(timeout int) {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.Flush(timeout)
}

func LastEventID() {
	hub, err := GetCurrentHub()
	if _, ok := err.(*NoHubError); ok {
		return
	}
	hub.LastEventID()
}
