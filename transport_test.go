package sentry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type unserializableType struct {
	UnsupportedField func()
}

const basicEvent = "{\"message\":\"mkey\",\"sdk\":{},\"user\":{}}"
const enhancedEvent = "{\"extra\":{\"info\":\"Original event couldn't be marshalled. Succeeded by stripping " +
	"the data that uses interface{} type. Please verify that the data you attach to the scope is serializable.\"}," +
	"\"message\":\"mkey\",\"sdk\":{},\"user\":{}}"

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

// A testHTTPServer counts events sent to it. It requires a call to Unblock
// before incrementing its internal counter and sending a response to the HTTP
// client. This allows for coordinating the execution flow when needed.
type testHTTPServer struct {
	*httptest.Server
	// eventCounter counts the number of events processed by the server.
	eventCounter *uint64
	// ch is used to block/unblock the server on demand.
	ch chan bool
}

func newTestHTTPServer(t *testing.T) *testHTTPServer {
	ch := make(chan bool)
	eventCounter := new(uint64)
	handler := func(w http.ResponseWriter, r *http.Request) {
		var event struct {
			EventID string `json:"event_id"`
		}
		dec := json.NewDecoder(r.Body)
		err := dec.Decode(&event)
		if err != nil {
			t.Fatal(err)
		}
		// Block until signal to continue.
		<-ch
		count := atomic.AddUint64(eventCounter, 1)
		t.Logf("[SERVER] {%.4s} event received (#%d)", event.EventID, count)
	}
	return &testHTTPServer{
		Server:       httptest.NewTLSServer(http.HandlerFunc(handler)),
		eventCounter: eventCounter,
		ch:           ch,
	}
}

func (ts *testHTTPServer) EventCount() uint64 {
	return atomic.LoadUint64(ts.eventCounter)
}

func (ts *testHTTPServer) Unblock() {
	ts.ch <- true
}

func TestHTTPTransport(t *testing.T) {
	server := newTestHTTPServer(t)
	defer server.Close()

	transport := NewHTTPTransport()
	transport.Configure(ClientOptions{
		Dsn:        fmt.Sprintf("https://test@%s/1", server.Listener.Addr()),
		HTTPClient: server.Client(),
	})

	// Helpers

	transportSendTestEvent := func(t *testing.T) (id string) {
		t.Helper()

		e := NewEvent()
		id = uuid()
		e.EventID = EventID(id)

		transport.SendEvent(e)
		t.Logf("[CLIENT] {%.4s} event sent", e.EventID)
		return id
	}

	transportMustFlush := func(t *testing.T, id string) {
		t.Helper()

		ok := transport.Flush(100 * time.Millisecond)
		if !ok {
			t.Fatalf("[CLIENT] {%.4s} Flush() timed out", id)
		}
	}

	serverEventCountMustBe := func(t *testing.T, n uint64) {
		t.Helper()

		count := server.EventCount()
		if count != n {
			t.Fatalf("[SERVER] event count = %d, want %d", count, n)
		}
	}

	// Actual tests

	testSendSingleEvent := func(t *testing.T) {
		// Sending a single event should increase the server event count by
		// exactly one.

		initialCount := server.EventCount()
		id := transportSendTestEvent(t)

		// Server is blocked waiting for us, right now count must not have
		// changed yet.
		serverEventCountMustBe(t, initialCount)

		// After unblocking the server, Flush must guarantee that the server
		// event count increased by one.
		server.Unblock()
		transportMustFlush(t, id)
		serverEventCountMustBe(t, initialCount+1)
	}
	t.Run("SendSingleEvent", testSendSingleEvent)

	t.Run("FlushMultipleTimes", func(t *testing.T) {
		// Flushing multiple times should not increase the server event count.

		initialCount := server.EventCount()
		for i := 0; i < 10; i++ {
			transportMustFlush(t, fmt.Sprintf("loop%d", i))
		}
		serverEventCountMustBe(t, initialCount)
	})

	t.Run("ConcurrentSendAndFlush", func(t *testing.T) {
		// It should be safe to send events and flush concurrently.

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			testSendSingleEvent(t)
			wg.Done()
		}()
		go func() {
			transportMustFlush(t, "from goroutine")
			wg.Done()
		}()
		wg.Wait()
	})
}
