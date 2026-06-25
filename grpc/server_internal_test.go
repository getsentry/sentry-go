package sentrygrpc

import (
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/metadata"
)

func TestMetadataToContext(t *testing.T) {
	tests := []struct {
		name   string
		client *sentry.Client
		md     metadata.MD
		want   map[string]any
	}{
		{
			name: "default snapshot filters values and keeps keys",
			client: func() *sentry.Client {
				client, err := sentry.NewClient(sentry.ClientOptions{Dsn: "https://key@sentry.io/1"})
				if err != nil {
					t.Fatal(err)
				}
				return client
			}(),
			md: metadata.MD{
				"authorization": []string{"Bearer secret"},
				"x-request-id":  []string{"req-123"},
			},
			want: map[string]any{
				"authorization": "[Filtered]",
				"x-request-id":  "req-123",
			},
		},
		{
			name: "explicit data collection filters values and keeps keys",
			client: func() *sentry.Client {
				client, err := sentry.NewClient(sentry.ClientOptions{
					Dsn: "https://key@sentry.io/1",
					DataCollection: &sentry.DataCollection{
						HTTPHeaders: &sentry.HeaderCollectionConfig{
							Request: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionDenyList},
						},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				return client
			}(),
			md: metadata.MD{
				"authorization": []string{"Bearer secret"},
				"x-request-id":  []string{"req-123"},
			},
			want: map[string]any{
				"authorization": "[Filtered]",
				"x-request-id":  "req-123",
			},
		},
		{
			name: "cookie metadata is filtered by cookie name",
			client: func() *sentry.Client {
				client, err := sentry.NewClient(sentry.ClientOptions{
					Dsn:            "https://key@sentry.io/1",
					DataCollection: &sentry.DataCollection{},
				})
				if err != nil {
					t.Fatal(err)
				}
				return client
			}(),
			md: metadata.MD{
				"cookie":       []string{"session=secret; theme=dark"},
				"set-cookie":   []string{"auth_token=secret; preference=blue"},
				"x-request-id": []string{"req-123"},
			},
			want: map[string]any{
				"cookie":       "session=[Filtered]; theme=dark",
				"set-cookie":   "auth_token=[Filtered]; preference=blue",
				"x-request-id": "req-123",
			},
		},
		{
			name: "cookie metadata can be disabled separately",
			client: func() *sentry.Client {
				client, err := sentry.NewClient(sentry.ClientOptions{
					Dsn: "https://key@sentry.io/1",
					DataCollection: &sentry.DataCollection{
						Cookies: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				return client
			}(),
			md: metadata.MD{
				"cookie":       []string{"session=secret"},
				"x-request-id": []string{"req-123"},
			},
			want: map[string]any{
				"x-request-id": "req-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, metadataToContext(tt.client, tt.md), testutils.EquateKeyValueStrings()); diff != "" {
				t.Errorf("metadata context mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
