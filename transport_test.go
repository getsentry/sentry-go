package sentry

import (
	"net/http"
	"testing"
	"time"
)

type unserializableType struct {
	UnsupportedField func()
}

const basicEvent = "{\"message\":\"mkey\",\"sdk\":{},\"user\":{},\"request\":{}}"
const enhancedEvent = "{\"extra\":{\"info\":\"Original event couldn't be marshalled. Succeeded by stripping " +
	"the data that uses interface{} type. Please verify that the data you attach to the scope is serializable.\"}," +
	"\"message\":\"mkey\",\"sdk\":{},\"user\":{},\"request\":{}}"

func TestGetRequestBodyFromEventValid(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Message: "mkey",
	})

	got := string(body)
	want := basicEvent

	if got != want {
		t.Errorf("expected different shape of body. \ngot: %s\nwant: %s", got, want)
	}
}

func TestGetRequestBodyFromEventInvalidBreadcrumbsField(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Message: "mkey",
		Breadcrumbs: []*Breadcrumb{{
			Data: map[string]interface{}{
				"wat": unserializableType{},
			},
		}},
	})

	got := string(body)
	want := enhancedEvent

	if got != want {
		t.Errorf("expected different shape of body. \ngot: %s\nwant: %s", got, want)
	}
}

func TestGetRequestBodyFromEventInvalidExtraField(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Message: "mkey",
		Extra: map[string]interface{}{
			"wat": unserializableType{},
		},
	})

	got := string(body)
	want := enhancedEvent

	if got != want {
		t.Errorf("expected different shape of body. \ngot: %s\nwant: %s", got, want)
	}
}

func TestGetRequestBodyFromEventInvalidContextField(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Message: "mkey",
		Contexts: map[string]interface{}{
			"wat": unserializableType{},
		},
	})

	got := string(body)
	want := enhancedEvent

	if got != want {
		t.Errorf("expected different shape of body. \ngot: %s\nwant: %s", got, want)
	}
}

func TestGetRequestBodyFromEventMultipleInvalidFields(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Message: "mkey",
		Breadcrumbs: []*Breadcrumb{{
			Data: map[string]interface{}{
				"wat": unserializableType{},
			},
		}},
		Extra: map[string]interface{}{
			"wat": unserializableType{},
		},
		Contexts: map[string]interface{}{
			"wat": unserializableType{},
		},
	})

	got := string(body)
	want := enhancedEvent

	if got != want {
		t.Errorf("expected different shape of body. \ngot: %s\nwant: %s", got, want)
	}
}

func TestGetRequestBodyFromEventCompletelyInvalid(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Exception: []Exception{{
			Stacktrace: &Stacktrace{
				Frames: []Frame{{
					Vars: map[string]interface{}{
						"wat": unserializableType{},
					},
				}},
			},
		}},
	})

	if body != nil {
		t.Error("expected body to be nil")
	}
}

func TestRetryAfterNoHeader(t *testing.T) {
	r := http.Response{}
	assertEqual(t, retryAfter(time.Now(), &r), time.Second*60)
}

func TestRetryAfterIncorrectHeader(t *testing.T) {
	r := http.Response{
		Header: map[string][]string{
			"Retry-After": {"x"},
		},
	}
	assertEqual(t, retryAfter(time.Now(), &r), time.Second*60)
}

func TestRetryAfterDelayHeader(t *testing.T) {
	r := http.Response{
		Header: map[string][]string{
			"Retry-After": {"1337"},
		},
	}
	assertEqual(t, retryAfter(time.Now(), &r), time.Second*1337)
}

func TestRetryAfterDateHeader(t *testing.T) {
	now, _ := time.Parse(time.RFC1123, "Wed, 21 Oct 2015 07:28:00 GMT")
	r := http.Response{
		Header: map[string][]string{
			"Retry-After": {"Wed, 21 Oct 2015 07:28:13 GMT"},
		},
	}
	assertEqual(t, retryAfter(now, &r), time.Second*13)
}
