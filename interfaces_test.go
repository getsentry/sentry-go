package sentry

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
)

const testPayload = `{"test_data": true}`

func TestRequest_FromHTTPRequest(t *testing.T) {
	t.Run("reading_body", func(t *testing.T) {
		payload := bytes.NewBufferString(testPayload)
		req, err := http.NewRequest("POST", "/test/", payload)
		assertEqual(t, err, nil)
		assertNotEqual(t, req, nil)
		sentryRequest := Request{}
		sentryRequest = sentryRequest.FromHTTPRequest(req)
		assertEqual(t, sentryRequest.Data, testPayload)
	})

	t.Run("reading_body_twice", func(t *testing.T) {
		payload := bytes.NewBufferString(testPayload)
		req, err := http.NewRequest("POST", "/test/", payload)
		assertEqual(t, err, nil)
		assertNotEqual(t, req, nil)
		sentryRequest := Request{}
		sentryRequest = sentryRequest.FromHTTPRequest(req)
		assertEqual(t, sentryRequest.Data, testPayload)

		// Re-reading original *http.Request.Body
		reqBody, err := ioutil.ReadAll(req.Body)
		assertEqual(t, err, nil)
		assertEqual(t, string(reqBody), testPayload)
	})
}
