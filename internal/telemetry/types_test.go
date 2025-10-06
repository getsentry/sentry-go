package telemetry

import "testing"

func TestOverflowPolicyString(t *testing.T) {
	testCases := []struct {
		policy   OverflowPolicy
		expected string
	}{
		{OverflowPolicyDropOldest, "drop_oldest"},
		{OverflowPolicyDropNewest, "drop_newest"},
		{OverflowPolicy(999), "unknown"},
	}

	for _, tc := range testCases {
		if got := tc.policy.String(); got != tc.expected {
			t.Errorf("Expected %s, got %s", tc.expected, got)
		}
	}
}
