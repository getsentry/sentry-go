package telemetry

import "testing"

func TestPriority_String(t *testing.T) {
	testCases := []struct {
		name     string
		priority Priority
		expected string
	}{
		{"critical", PriorityCritical, "critical"},
		{"high", PriorityHigh, "high"},
		{"medium", PriorityMedium, "medium"},
		{"low", PriorityLow, "low"},
		{"lowest", PriorityLowest, "lowest"},
		{"unknown", Priority(999), "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.priority.String(); got != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, got)
			}
		})
	}
}

func TestDataCategory_String(t *testing.T) {
	testCases := []struct {
		name     string
		category DataCategory
		expected string
	}{
		{"error", DataCategoryError, "error"},
		{"transaction", DataCategoryTransaction, "transaction"},
		{"checkin", DataCategoryCheckIn, "checkin"},
		{"log", DataCategoryLog, "log"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.category.String(); got != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, got)
			}
		})
	}
}

func TestDataCategory_GetPriority(t *testing.T) {
	testCases := []struct {
		name             string
		category         DataCategory
		expectedPriority Priority
	}{
		{"error", DataCategoryError, PriorityCritical},
		{"checkin", DataCategoryCheckIn, PriorityHigh},
		{"log", DataCategoryLog, PriorityMedium},
		{"transaction", DataCategoryTransaction, PriorityLow},
		{"unknown", DataCategory("unknown"), PriorityMedium},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.category.GetPriority(); got != tc.expectedPriority {
				t.Errorf("Expected %s, got %s", tc.expectedPriority, got)
			}
		})
	}
}

func TestPriorityConstants(t *testing.T) {
	// Test that priority constants have the expected values
	expectedValues := map[Priority]int{
		PriorityCritical: 1,
		PriorityHigh:     2,
		PriorityMedium:   3,
		PriorityLow:      4,
		PriorityLowest:   5,
	}

	for priority, expectedValue := range expectedValues {
		if int(priority) != expectedValue {
			t.Errorf("Expected %s to have value %d, got %d", priority, expectedValue, int(priority))
		}
	}
}

func TestDataCategoryConstants(t *testing.T) {
	// Test that data category constants have the expected string values
	expectedValues := map[DataCategory]string{
		DataCategoryError:       "error",
		DataCategoryTransaction: "transaction",
		DataCategoryCheckIn:     "checkin",
		DataCategoryLog:         "log",
	}

	for category, expectedValue := range expectedValues {
		if string(category) != expectedValue {
			t.Errorf("Expected %s to have string value %s, got %s", category, expectedValue, string(category))
		}
	}
}

func TestPriorityOrdering(t *testing.T) {
	// Test that priorities are ordered correctly (lower value = higher priority)
	priorities := []Priority{
		PriorityCritical,
		PriorityHigh,
		PriorityMedium,
		PriorityLow,
		PriorityLowest,
	}

	for i := 1; i < len(priorities); i++ {
		if priorities[i-1] >= priorities[i] {
			t.Errorf("Priority %s should be higher than %s", priorities[i-1], priorities[i])
		}
	}
}

func TestCriticalPriorityCategories(t *testing.T) {
	// Test that error and feedback categories have critical priority
	criticalCategories := []DataCategory{
		DataCategoryError,
	}

	for _, category := range criticalCategories {
		if category.GetPriority() != PriorityCritical {
			t.Errorf("Category %s should have critical priority, got %s", category, category.GetPriority())
		}
	}
}

func TestHighPriorityCategories(t *testing.T) {
	// Test that session and check-in categories have high priority
	highCategories := []DataCategory{
		DataCategoryCheckIn,
	}

	for _, category := range highCategories {
		if category.GetPriority() != PriorityHigh {
			t.Errorf("Category %s should have high priority, got %s", category, category.GetPriority())
		}
	}
}

func TestMediumPriorityCategories(t *testing.T) {
	// Test that log and span categories have medium priority
	mediumCategories := []DataCategory{
		DataCategoryLog,
	}

	for _, category := range mediumCategories {
		if category.GetPriority() != PriorityMedium {
			t.Errorf("Category %s should have medium priority, got %s", category, category.GetPriority())
		}
	}
}

func TestLowPriorityCategories(t *testing.T) {
	// Test that transaction and profile categories have low priority
	lowCategories := []DataCategory{
		DataCategoryTransaction,
	}

	for _, category := range lowCategories {
		if category.GetPriority() != PriorityLow {
			t.Errorf("Category %s should have low priority, got %s", category, category.GetPriority())
		}
	}
}

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
