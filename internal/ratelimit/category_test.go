package ratelimit

import (
	"testing"
)

func TestKnownCategories(t *testing.T) {
	expectedCategories := []Category{
		CategoryAll,
		CategoryError,
		CategoryTransaction,
		CategoryMonitor,
		CategoryLog,
		CategoryTraceMetric,
	}

	for _, category := range expectedCategories {
		t.Run(string(category), func(t *testing.T) {
			if _, exists := knownCategories[category]; !exists {
				t.Errorf("Category %q should be in knownCategories map", category)
			}
		})
	}

	// Test that unknown categories are not in the map
	unknownCategories := []Category{
		Category("unknown"),
		Category("custom"),
		Category("random"),
	}

	for _, category := range unknownCategories {
		t.Run("unknown_"+string(category), func(t *testing.T) {
			if _, exists := knownCategories[category]; exists {
				t.Errorf("Unknown category %q should not be in knownCategories map", category)
			}
		})
	}
}
