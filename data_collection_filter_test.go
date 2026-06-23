package sentry

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIsSensitiveKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		// Exact terms
		{"auth", true},
		{"token", true},
		{"secret", true},
		{"password", true},
		{"passwd", true},
		{"pwd", true},
		{"key", true},
		{"jwt", true},
		{"bearer", true},
		{"sso", true},
		{"saml", true},
		{"csrf", true},
		{"xsrf", true},
		{"credentials", true},
		{"session", true},
		{"sid", true},
		{"identity", true},

		// Substring matches (case-insensitive)
		{"Authorization", true},
		{"X-Auth-Token", true},
		{"X-API-Key", true},
		{"x-csrf-token", true},
		{"XSRF-TOKEN", true},
		{"session-id", true},
		{"user-session", true},
		{"Proxy-Authorization", true},
		{"x-api-key", true},
		{"private-key", true},
		{"JWT-Token", true},
		{"Bearer-Auth", true},

		// Non-sensitive
		{"Content-Type", false},
		{"Accept", false},
		{"Host", false},
		{"User-Agent", false},
		{"X-Request-Id", false},
		{"Cache-Control", false},
		{"Content-Length", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			got := isSensitiveKey(tt.key)
			if got != tt.want {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestFilterKeyValues(t *testing.T) {
	t.Parallel()

	data := map[string]string{
		"Authorization": "Bearer abc123",
		"Content-Type":  "application/json",
		"X-Request-Id":  "req-456",
		"X-Api-Key":     "secret-key",
	}

	t.Run("nil behavior uses DenyList default", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(data, nil)
		if result["Authorization"] != filteredValue {
			t.Error("Authorization should be filtered")
		}
		if result["Content-Type"] != "application/json" {
			t.Error("Content-Type should not be filtered")
		}
		if result["X-Api-Key"] != filteredValue {
			t.Error("X-Api-Key should be filtered")
		}
		if result["X-Request-Id"] != "req-456" {
			t.Error("X-Request-Id should not be filtered")
		}
	})

	t.Run("DenyList mode", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(data, &KeyValueCollectionBehavior{Mode: CollectionDenyList})
		if result["Authorization"] != filteredValue {
			t.Error("Authorization should be filtered")
		}
		if result["Content-Type"] != "application/json" {
			t.Error("Content-Type should not be filtered")
		}
	})

	t.Run("DenyList with extra terms", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(data, &KeyValueCollectionBehavior{
			Mode:  CollectionDenyList,
			Terms: []string{"request-id"},
		})
		if result["X-Request-Id"] != filteredValue {
			t.Error("X-Request-Id should be filtered by extra term")
		}
		if result["Content-Type"] != "application/json" {
			t.Error("Content-Type should not be filtered")
		}
	})

	t.Run("AllowList mode", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(data, &KeyValueCollectionBehavior{
			Mode:  CollectionAllowList,
			Terms: []string{"content-type", "x-request-id"},
		})
		if result["Content-Type"] != "application/json" {
			t.Error("Content-Type should pass through allow list")
		}
		if result["X-Request-Id"] != "req-456" {
			t.Error("X-Request-Id should pass through allow list")
		}
		if result["Authorization"] != filteredValue {
			t.Error("Authorization should be filtered (not in allow list + sensitive)")
		}
		if result["X-Api-Key"] != filteredValue {
			t.Error("X-Api-Key should be filtered (not in allow list + sensitive)")
		}
	})

	t.Run("AllowList sensitive keys still scrubbed", func(t *testing.T) {
		t.Parallel()
		// Even if "authorization" is in the allow list, it matches the
		// sensitive denylist and MUST be scrubbed.
		result := filterKeyValues(data, &KeyValueCollectionBehavior{
			Mode:  CollectionAllowList,
			Terms: []string{"authorization"},
		})
		if result["Authorization"] != filteredValue {
			t.Error("Authorization should be filtered even when in allow list")
		}
	})

	t.Run("Off mode", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(data, &KeyValueCollectionBehavior{Mode: CollectionOff})
		if result != nil {
			t.Errorf("Off mode should return nil, got %v", result)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		result := filterKeyValues(map[string]string{}, &KeyValueCollectionBehavior{Mode: CollectionDenyList})
		if result == nil {
			t.Error("should return non-nil empty map")
		}
		if len(result) != 0 {
			t.Errorf("should return empty map, got %v", result)
		}
	})
}

func TestCollectHTTPBody(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := make([]BodyType, 0)
			for _, bt := range allBodyTypes() {
				if tt.dc.CollectHTTPBody(bt) {
					got = append(got, bt)
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("collected body types mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPrivacyDenyTerms(t *testing.T) {
	t.Parallel()

	got := PrivacyDenyTerms()
	want := []string{"forwarded", "-ip", "remote-", "via", "-user"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("privacy deny terms mismatch (-want +got):\n%s", diff)
	}

	got[0] = "changed"
	if diff := cmp.Diff(want, PrivacyDenyTerms()); diff != "" {
		t.Errorf("privacy deny terms should return a copy (-want +got):\n%s", diff)
	}
}

func TestPrivacyDenyTermsAreOptIn(t *testing.T) {
	t.Parallel()

	data := map[string]string{
		"x-forwarded-for": "192.0.2.1",
		"x-real-ip":       "192.0.2.2",
		"remote-addr":     "192.0.2.3",
		"x-user-id":       "user-123",
	}

	defaultFiltered := filterKeyValues(data, nil)
	for key, value := range data {
		if defaultFiltered[key] != value {
			t.Errorf("%s should not be filtered by default, got %q", key, defaultFiltered[key])
		}
	}

	strictFiltered := filterKeyValues(data, &KeyValueCollectionBehavior{
		Mode:  CollectionDenyList,
		Terms: PrivacyDenyTerms(),
	})
	for key := range data {
		if strictFiltered[key] != filteredValue {
			t.Errorf("%s should be filtered with privacy deny terms, got %q", key, strictFiltered[key])
		}
	}
}
