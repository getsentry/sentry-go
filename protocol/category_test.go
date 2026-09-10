package protocol

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
		{CategoryTraceMetric, "CategoryTraceMetric"},
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
