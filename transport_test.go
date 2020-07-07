package sentry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type unserializableType struct {
	UnsupportedField func()
}

//nolint: lll
const (
	basicEvent                         = `{"message":"mkey","sdk":{},"user":{}}`
	enhancedEventInvalidBreadcrumb     = `{"extra":{"info":"Could not encode original event as JSON. Succeeded by removing Breadcrumbs, Contexts and Extra. Please verify the data you attach to the scope. Error: json: error calling MarshalJSON for type *sentry.Event: json: error calling MarshalJSON for type *sentry.Breadcrumb: json: unsupported type: func()"},"message":"mkey","sdk":{},"user":{}}`
	enhancedEventInvalidContextOrExtra = `{"extra":{"info":"Could not encode original event as JSON. Succeeded by removing Breadcrumbs, Contexts and Extra. Please verify the data you attach to the scope. Error: json: error calling MarshalJSON for type *sentry.Event: json: unsupported type: func()"},"message":"mkey","sdk":{},"user":{}}`
)

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
	want := enhancedEventInvalidBreadcrumb

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
	want := enhancedEventInvalidContextOrExtra

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
	want := enhancedEventInvalidContextOrExtra

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
	want := enhancedEventInvalidBreadcrumb

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

func TestGetEnvelopeFromBody(t *testing.T) {
	body := getRequestBodyFromEvent(&Event{
		Type:           transactionType,
		Spans:          []*Span{},
		StartTimestamp: time.Unix(3, 0).UTC(),
		Timestamp:      time.Unix(5, 0).UTC(),
	})
	env := getEnvelopeFromBody(body, time.Unix(6, 0))
	got := env.String()
	//nolint: lll
	want := strings.Join([]string{
		`{"sent_at":"1970-01-01T00:00:06Z"}`,
		`{"type":"transaction"}`,
		`{"sdk":{},"timestamp":"1970-01-01T00:00:05Z","user":{},"type":"transaction","start_timestamp":"1970-01-01T00:00:03Z"}`,
	}, "\n")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Event mismatch (-want +got):\n%s", diff)
	}
}

func TestGetRequestFromEvent(t *testing.T) {
	testCases := []struct {
		testName string
		// input
		event *Event
		// output
		apiURL string
	}{
		{
			testName: "Sample Event",
			event:    NewEvent(),
			apiURL:   "https://host/path/api/42/store/",
		},
		{
			testName: "Transaction",
			event: func() *Event {
				event := NewEvent()
				event.Type = transactionType

				return event
			}(),
			apiURL: "https://host/path/api/42/envelope/",
		},
	}

	for _, test := range testCases {
		test := test
		dsn, err := NewDsn("https://key@host/path/42")
		if err != nil {
			t.Fatal(err)
		}

		t.Run(test.testName, func(t *testing.T) {
			req, err := getRequestFromEvent(test.event, dsn)
			if err != nil {
				t.Fatal(err)
			}

			if req.Method != http.MethodPost {
				t.Errorf("Request %v using http method: %s, supposed to use method: %s", req, req.Method, http.MethodPost)
			}

			if req.URL.String() != test.apiURL {
				t.Errorf("Incorrect API URL. want: %s, got: %s", test.apiURL, req.URL.String())
			}
		})
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
