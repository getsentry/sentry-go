package sentry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type Transport interface {
	// Flush(timeout int) chan error
	Configure(options ClientOptions)
	SendEvent(event *Event) (*http.Response, error)
}

type HTTPTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport *http.Transport
}

func (t *HTTPTransport) Configure(options ClientOptions) {
	// TODO: Implement proxies/ca_certs here
	dsn, _ := NewDsn(options.Dsn)
	t.dsn = dsn
	t.transport = &http.Transport{}
	t.client = &http.Client{
		Transport: t.transport,
	}
}

func (t *HTTPTransport) SendEvent(event *Event) (*http.Response, error) {
	if t.dsn == nil {
		return nil, nil
	}

	body, _ := json.Marshal(event)

	dbg, _ := json.MarshalIndent(event, "", "  ")
	fmt.Println(string(dbg))

	request, _ := http.NewRequest(
		http.MethodPost,
		t.dsn.StoreAPIURL().String(),
		bytes.NewBuffer(body),
	)

	for headerKey, headerValue := range t.dsn.RequestHeaders() {
		request.Header.Set(headerKey, headerValue)
	}

	response, err := t.client.Do(request)

	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	body, _ = ioutil.ReadAll(response.Body)
	fmt.Println(string(body))

	return response, err
}
