package sentry

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewClientDataCollection(t *testing.T) {
	t.Parallel()

	specDefaults := DataCollection{
		UserInfo:    Set(true),
		Cookies:     &KeyValueCollectionBehavior{Mode: CollectionDenyList},
		HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionDenyList}, Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList}},
		HTTPBodies:  allBodyTypes(),
		QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList},
	}
	legacyPrivacyDefaults := DataCollection{
		UserInfo:    Set(false),
		Cookies:     &KeyValueCollectionBehavior{Mode: CollectionOff},
		HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: PrivacyDenyTerms()}, Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: PrivacyDenyTerms()}},
		HTTPBodies:  []BodyType{},
		QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: PrivacyDenyTerms()},
	}

	tests := []struct {
		name    string
		options ClientOptions
		want    DataCollection
	}{
		{
			name: "empty explicit config applies spec defaults",
			options: ClientOptions{
				Dsn:            "https://key@sentry.io/1",
				DataCollection: &DataCollection{},
			},
			want: specDefaults,
		},
		{
			name: "nil config with SendDefaultPII true applies spec defaults",
			options: ClientOptions{
				Dsn:            "https://key@sentry.io/1",
				SendDefaultPII: true,
			},
			want: specDefaults,
		},
		{
			name: "nil config with SendDefaultPII false applies legacy privacy compatibility",
			options: ClientOptions{
				Dsn: "https://key@sentry.io/1",
			},
			want: legacyPrivacyDefaults,
		},
		{
			name: "zero-value key-value behaviors use default deny list",
			options: ClientOptions{
				Dsn: "https://key@sentry.io/1",
				DataCollection: &DataCollection{
					Cookies:     &KeyValueCollectionBehavior{},
					HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{}, Response: &KeyValueCollectionBehavior{}},
					QueryParams: &KeyValueCollectionBehavior{},
				},
			},
			want: specDefaults,
		},
		{
			name: "explicit overrides are preserved and missing fields default",
			options: ClientOptions{
				Dsn: "https://key@sentry.io/1",
				DataCollection: &DataCollection{
					UserInfo:    Set(false),
					Cookies:     &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}},
					HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"x-request-id"}}},
					HTTPBodies:  []BodyType{},
				},
				SendDefaultPII: true,
			},
			want: DataCollection{
				UserInfo:    Set(false),
				Cookies:     &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}},
				HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"x-request-id"}}, Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList}},
				HTTPBodies:  []BodyType{},
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList},
			},
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

	input := &DataCollection{
		UserInfo:   Set(false),
		Cookies:    &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}},
		HTTPBodies: []BodyType{BodyIncomingRequest},
	}
	client, err := NewClient(ClientOptions{
		Dsn:            "https://key@sentry.io/1",
		DataCollection: input,
	})
	if err != nil {
		t.Fatal(err)
	}

	input.UserInfo = Set(true)
	input.Cookies.Mode = CollectionOff
	input.Cookies.Terms[0] = "changed"
	input.HTTPBodies[0] = BodyOutgoingResponse

	returned := client.GetDataCollection()
	returned.UserInfo = Set(true)
	returned.Cookies.Mode = CollectionOff
	returned.Cookies.Terms[0] = "changed"
	returned.HTTPHeaders.Request.Mode = CollectionAllowList
	returned.HTTPBodies[0] = BodyOutgoingResponse
	returned.QueryParams.Mode = CollectionOff

	want := DataCollection{
		UserInfo:    Set(false),
		Cookies:     &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"session_id"}},
		HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionDenyList}, Response: &KeyValueCollectionBehavior{Mode: CollectionDenyList}},
		HTTPBodies:  []BodyType{BodyIncomingRequest},
		QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList},
	}

	if diff := cmp.Diff(want, client.GetDataCollection()); diff != "" {
		t.Errorf("client data collection should be snapshotted (-want +got):\n%s", diff)
	}
}

func TestNewClientLegacyDataCollectionPrivacyTermsAreNotShared(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientOptions{Dsn: "https://key@sentry.io/1"})
	if err != nil {
		t.Fatal(err)
	}

	got := client.GetDataCollection()
	got.HTTPHeaders.Request.Terms[0] = "changed"

	if diff := cmp.Diff(PrivacyDenyTerms(), got.HTTPHeaders.Response.Terms); diff != "" {
		t.Errorf("response header terms should not share request header terms (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(PrivacyDenyTerms(), got.QueryParams.Terms); diff != "" {
		t.Errorf("query param terms should not share request header terms (-want +got):\n%s", diff)
	}
}
