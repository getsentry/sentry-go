package ratelimit

import (
	"testing"
)

func TestCategory_String(t *testing.T) {
	tests := []struct {
		category Category
		expected string
	}{
		{CategoryAll, "CategoryAll"},
		{CategoryError, "CategoryError"},
		{CategoryTransaction, "CategoryTransaction"},
		{CategoryMonitor, "CategoryMonitor"},
		{CategoryLog, "CategoryLog"},
		{Category("custom type"), "CategoryCustomType"},
		{Category("multi word type"), "CategoryMultiWordType"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			result := tt.category.String()
			if result != tt.expected {
				t.Errorf("Category(%q).String() = %q, want %q", tt.category, result, tt.expected)
			}
		})
	}
}

func TestKnownCategories(t *testing.T) {
	expectedCategories := []Category{
		CategoryAll,
		CategoryError,
		CategoryTransaction,
		CategoryMonitor,
		CategoryLog,
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

func TestPriority_String(t *testing.T) {
	tests := []struct {
		priority Priority
		expected string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityMedium, "medium"},
		{PriorityLow, "low"},
		{PriorityLowest, "lowest"},
		{Priority(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.priority.String(); got != tt.expected {
			t.Errorf("Priority(%d).String() = %q, want %q", tt.priority, got, tt.expected)
		}
	}
}

func TestCategory_GetPriority(t *testing.T) {
	tests := []struct {
		category Category
		expected Priority
	}{
		{CategoryError, PriorityCritical},
		{CategoryMonitor, PriorityHigh},
		{CategoryLog, PriorityLow},
		{CategoryTransaction, PriorityMedium},
		{Category("unknown"), PriorityMedium},
	}

	for _, tt := range tests {
		if got := tt.category.GetPriority(); got != tt.expected {
			t.Errorf("Category(%q).GetPriority() = %s, want %s", tt.category, got, tt.expected)
		}
	}
}
