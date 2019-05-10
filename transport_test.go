package sentry

import (
	"net/http"
	"testing"
	"time"
)

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
