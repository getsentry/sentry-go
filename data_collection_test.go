package sentry

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func defaultResolvedDataCollection() DataCollection {
	return DataCollection{
		UserInfo:            Set(true),
		Cookies:             &KeyValueCollectionBehavior{Mode: CollectionDenyList},
		HTTPHeaders:         &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionDenyList}, Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList}},
		HTTPBodies:          allBodyTypes(),
		QueryParams:         &KeyValueCollectionBehavior{Mode: CollectionDenyList},
	}
}

func TestNewDataCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          *DataCollection
		sendDefaultPII bool
		want           DataCollection
	}{
		{
			name: "defaults",
			want: defaultResolvedDataCollection(),
		},
		{
			name: "explicit overrides",
			input: &DataCollection{
				UserInfo: Set(false),
				Cookies: &KeyValueCollectionBehavior{
					Mode:  CollectionAllowList,
					Terms: []string{"session_id"},
				},
				HTTPBodies: []BodyType{BodyIncomingRequest},
			},
			want: func() DataCollection {
				want := defaultResolvedDataCollection()
				want.UserInfo = Set(false)
				want.Cookies = &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}}
				want.HTTPBodies = []BodyType{BodyIncomingRequest}
				return want
			}(),
		},
		{
			name: "empty HTTPBodies is explicit off",
			input: &DataCollection{
				HTTPBodies: []BodyType{},
			},
			want: func() DataCollection {
				want := defaultResolvedDataCollection()
				want.HTTPBodies = []BodyType{}
				return want
			}(),
		},
		{
			name:           "legacy SendDefaultPII still resolves to defaults",
			sendDefaultPII: true,
			want:           defaultResolvedDataCollection(),
		},
		{
			name: "explicit overrides take precedence over SendDefaultPII",
			input: &DataCollection{
				UserInfo: Set(false),
			},
			sendDefaultPII: true,
			want: func() DataCollection {
				want := defaultResolvedDataCollection()
				want.UserInfo = Set(false)
				return want
			}(),
		},
		{
			name: "partial header config defaults missing direction",
			input: &DataCollection{
				HTTPHeaders: &HeaderCollectionConfig{
					Request: &KeyValueCollectionBehavior{
						Mode:  CollectionAllowList,
						Terms: []string{"x-request-id"},
					},
				},
			},
			want: func() DataCollection {
				want := defaultResolvedDataCollection()
				want.HTTPHeaders = &HeaderCollectionConfig{
					Request:  &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"x-request-id"}},
					Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList},
				}
				return want
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := newDataCollection(tt.input, tt.sendDefaultPII)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("resolved data collection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIsHTTPBodyCollected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dc   *DataCollection
		want []BodyType
	}{
		{name: "nil DataCollection collects all", dc: nil, want: allBodyTypes()},
		{name: "nil HTTPBodies collects all", dc: &DataCollection{}, want: allBodyTypes()},
		{name: "empty HTTPBodies collects none", dc: &DataCollection{HTTPBodies: []BodyType{}}, want: []BodyType{}},
		{name: "specific types included", dc: &DataCollection{HTTPBodies: []BodyType{BodyIncomingRequest, BodyOutgoingRequest}}, want: []BodyType{BodyIncomingRequest, BodyOutgoingRequest}},
		{name: "single specific type included", dc: &DataCollection{HTTPBodies: []BodyType{BodyIncomingRequest}}, want: []BodyType{BodyIncomingRequest}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := make([]BodyType, 0)
			for _, bt := range allBodyTypes() {
				if tt.dc.isHTTPBodyCollected(bt) {
					got = append(got, bt)
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("collected body types mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewClientDataCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options ClientOptions
		want    DataCollection
	}{
		{
			name: "defaults applied",
			options: ClientOptions{
				Dsn: "https://key@sentry.io/1",
			},
			want: defaultResolvedDataCollection(),
		},
		{
			name: "explicit config resolved",
			options: ClientOptions{
				Dsn: "https://key@sentry.io/1",
				DataCollection: &DataCollection{
					UserInfo:   Set(false),
					HTTPBodies: []BodyType{},
				},
			},
			want: func() DataCollection {
				want := defaultResolvedDataCollection()
				want.UserInfo = Set(false)
				want.HTTPBodies = []BodyType{}
				return want
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(tt.options)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tt.want, client.GetDataCollection()); diff != "" {
				t.Errorf("client data collection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewClientDataCollectionSnapshotting(t *testing.T) {
	t.Parallel()

	t.Run("input mutation after NewClient does not affect client", func(t *testing.T) {
		t.Parallel()

		input := &DataCollection{
			UserInfo:   Set(false),
			HTTPBodies: []BodyType{BodyIncomingRequest},
			Cookies: &KeyValueCollectionBehavior{
				Mode:  CollectionAllowList,
				Terms: []string{"session_id"},
			},
		}
		client, err := NewClient(ClientOptions{
			Dsn:            "https://key@sentry.io/1",
			DataCollection: input,
		})
		if err != nil {
			t.Fatal(err)
		}

		input.UserInfo = Set(true)
		input.HTTPBodies[0] = BodyOutgoingResponse
		input.Cookies.Mode = CollectionOff
		input.Cookies.Terms[0] = "changed"

		want := defaultResolvedDataCollection()
		want.UserInfo = Set(false)
		want.Cookies = &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}}
		want.HTTPBodies = []BodyType{BodyIncomingRequest}

		if diff := cmp.Diff(want, client.GetDataCollection()); diff != "" {
			t.Errorf("snapshot should not track caller mutations (-want +got):\n%s", diff)
		}
	})

	t.Run("returned config mutation does not affect client", func(t *testing.T) {
		t.Parallel()

		client, err := NewClient(ClientOptions{Dsn: "https://key@sentry.io/1"})
		if err != nil {
			t.Fatal(err)
		}

		got := client.GetDataCollection()
		got.UserInfo = Set(false)
		got.HTTPBodies[0] = BodyOutgoingResponse
		got.Cookies.Mode = CollectionOff
		got.HTTPHeaders.Request.Mode = CollectionAllowList
		got.QueryParams.Mode = CollectionOff

		if diff := cmp.Diff(defaultResolvedDataCollection(), client.GetDataCollection()); diff != "" {
			t.Errorf("returned config mutation should not affect client (-want +got):\n%s", diff)
		}
	})
}

func TestCollectionModeConstants(t *testing.T) {
	t.Parallel()

	got := []CollectionMode{CollectionOff, CollectionDenyList, CollectionAllowList}
	want := []CollectionMode{"off", "denyList", "allowList"}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("collection mode constants mismatch (-want +got):\n%s", diff)
	}
}

func TestBodyTypeConstants(t *testing.T) {
	t.Parallel()

	got := []BodyType{BodyIncomingRequest, BodyOutgoingRequest, BodyIncomingResponse, BodyOutgoingResponse}
	want := []BodyType{"incomingRequest", "outgoingRequest", "incomingResponse", "outgoingResponse"}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("body type constants mismatch (-want +got):\n%s", diff)
	}
}
