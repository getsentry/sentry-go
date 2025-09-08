package telemetry

import (
	"testing"
	"time"
)

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
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.priority.String(); got != tt.expected {
				t.Errorf("Priority.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDataCategory_String(t *testing.T) {
	tests := []struct {
		category DataCategory
		expected string
	}{
		{DataCategoryError, "error"},
		{DataCategoryTransaction, "transaction"},
		{DataCategorySession, "session"},
		{DataCategoryCheckIn, "checkin"},
		{DataCategoryLog, "log"},
		{DataCategorySpan, "span"},
		{DataCategoryProfile, "profile"},
		{DataCategoryReplay, "replay"},
		{DataCategoryFeedback, "feedback"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			if got := tt.category.String(); got != tt.expected {
				t.Errorf("DataCategory.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDataCategory_GetPriority(t *testing.T) {
	tests := []struct {
		category DataCategory
		expected Priority
	}{
		{DataCategoryError, PriorityCritical},
		{DataCategoryFeedback, PriorityCritical},
		{DataCategorySession, PriorityHigh},
		{DataCategoryCheckIn, PriorityHigh},
		{DataCategoryLog, PriorityMedium},
		{DataCategorySpan, PriorityMedium},
		{DataCategoryTransaction, PriorityLow},
		{DataCategoryProfile, PriorityLow},
		{DataCategoryReplay, PriorityLowest},
		{DataCategory("unknown"), PriorityMedium}, // Default case
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			if got := tt.category.GetPriority(); got != tt.expected {
				t.Errorf("DataCategory.GetPriority() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOverflowPolicy_String(t *testing.T) {
	tests := []struct {
		policy   OverflowPolicy
		expected string
	}{
		{OverflowPolicyDropOldest, "drop_oldest"},
		{OverflowPolicyDropNewest, "drop_newest"},
		{OverflowPolicy(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.policy.String(); got != tt.expected {
				t.Errorf("OverflowPolicy.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultTelemetryBufferConfig(t *testing.T) {
	config := DefaultTelemetryBufferConfig()

	// Test that config is not nil and has expected structure
	if config == nil {
		t.Fatal("DefaultTelemetryBufferConfig() returned nil")
	}

	// Test that all maps are initialized
	if config.BufferCapacities == nil {
		t.Error("BufferCapacities should be initialized")
	}
	if config.PriorityWeights == nil {
		t.Error("PriorityWeights should be initialized")
	}
	if config.BatchSizes == nil {
		t.Error("BatchSizes should be initialized")
	}
	if config.BatchTimeouts == nil {
		t.Error("BatchTimeouts should be initialized")
	}
	if config.OverflowPolicies == nil {
		t.Error("OverflowPolicies should be initialized")
	}

	// Test default enabled state
	if config.Enabled {
		t.Error("default config should have Enabled=false for gradual rollout")
	}

	// Test some specific default values
	if config.BufferCapacities[DataCategoryError] != 100 {
		t.Errorf("expected error buffer capacity 100, got %d",
			config.BufferCapacities[DataCategoryError])
	}

	if config.PriorityWeights[PriorityCritical] != 5 {
		t.Errorf("expected critical priority weight 5, got %d",
			config.PriorityWeights[PriorityCritical])
	}

	if config.BatchSizes[DataCategoryError] != 1 {
		t.Errorf("expected error batch size 1, got %d",
			config.BatchSizes[DataCategoryError])
	}

	if config.BatchTimeouts[DataCategoryError] != 0 {
		t.Errorf("expected error batch timeout 0, got %v",
			config.BatchTimeouts[DataCategoryError])
	}

	if config.OverflowPolicies[DataCategoryError] != OverflowPolicyDropOldest {
		t.Errorf("expected error overflow policy drop_oldest, got %v",
			config.OverflowPolicies[DataCategoryError])
	}

	// Test that all categories are configured
	expectedCategories := []DataCategory{
		DataCategoryError, DataCategoryTransaction, DataCategorySession,
		DataCategoryCheckIn, DataCategoryLog, DataCategorySpan,
		DataCategoryProfile, DataCategoryReplay, DataCategoryFeedback,
	}

	for _, category := range expectedCategories {
		if _, exists := config.BufferCapacities[category]; !exists {
			t.Errorf("missing buffer capacity for category %s", category)
		}
		if _, exists := config.BatchSizes[category]; !exists {
			t.Errorf("missing batch size for category %s", category)
		}
		if _, exists := config.BatchTimeouts[category]; !exists {
			t.Errorf("missing batch timeout for category %s", category)
		}
		if _, exists := config.OverflowPolicies[category]; !exists {
			t.Errorf("missing overflow policy for category %s", category)
		}
	}

	// Test that all priorities are configured
	expectedPriorities := []Priority{
		PriorityCritical, PriorityHigh, PriorityMedium,
		PriorityLow, PriorityLowest,
	}

	for _, priority := range expectedPriorities {
		if _, exists := config.PriorityWeights[priority]; !exists {
			t.Errorf("missing weight for priority %s", priority)
		}
	}
}

func TestTelemetryBufferConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultTelemetryBufferConfig()
		if err := config.Validate(); err != nil {
			t.Errorf("valid config should not return error: %v", err)
		}
	})

	t.Run("nil maps get initialized", func(t *testing.T) {
		config := &BufferConfig{
			Enabled: true,
		}

		if err := config.Validate(); err != nil {
			t.Errorf("config with nil maps should be auto-initialized: %v", err)
		}

		// Check that maps were initialized
		if config.BufferCapacities == nil {
			t.Error("BufferCapacities should be initialized")
		}
		if config.PriorityWeights == nil {
			t.Error("PriorityWeights should be initialized")
		}
	})

	t.Run("invalid buffer capacity", func(t *testing.T) {
		config := DefaultTelemetryBufferConfig()
		config.BufferCapacities[DataCategoryError] = 0

		err := config.Validate()
		if err == nil {
			t.Error("should return error for zero buffer capacity")
		}

		configErr, ok := err.(*ConfigValidationError)
		if !ok {
			t.Errorf("should return ConfigValidationError, got %T", err)
		}
		if configErr.Field != "BufferCapacities" {
			t.Errorf("expected field 'BufferCapacities', got %s", configErr.Field)
		}
	})

	t.Run("invalid priority weight", func(t *testing.T) {
		config := DefaultTelemetryBufferConfig()
		config.PriorityWeights[PriorityCritical] = -1

		err := config.Validate()
		if err == nil {
			t.Error("should return error for negative priority weight")
		}

		configErr, ok := err.(*ConfigValidationError)
		if !ok {
			t.Errorf("should return ConfigValidationError, got %T", err)
		}
		if configErr.Field != "PriorityWeights" {
			t.Errorf("expected field 'PriorityWeights', got %s", configErr.Field)
		}
	})

	t.Run("batch size exceeds capacity", func(t *testing.T) {
		config := DefaultTelemetryBufferConfig()
		config.BufferCapacities[DataCategoryLog] = 10
		config.BatchSizes[DataCategoryLog] = 20

		err := config.Validate()
		if err == nil {
			t.Error("should return error when batch size exceeds capacity")
		}

		configErr, ok := err.(*ConfigValidationError)
		if !ok {
			t.Errorf("should return ConfigValidationError, got %T", err)
		}
		if configErr.Field != "BatchSizes" {
			t.Errorf("expected field 'BatchSizes', got %s", configErr.Field)
		}
	})
}

func TestConfigValidationError(t *testing.T) {
	err := &ConfigValidationError{
		Field:   "TestField",
		Message: "test message",
	}

	expected := "telemetry config validation error in TestField: test message"
	if got := err.Error(); got != expected {
		t.Errorf("ConfigValidationError.Error() = %v, want %v", got, expected)
	}
}

func TestNewTelemetryMetrics(t *testing.T) {
	metrics := NewTelemetryMetrics()

	if metrics == nil {
		t.Fatal("NewTelemetryMetrics() returned nil")
	}

	// Test that all maps are initialized
	if metrics.EventsOffered == nil {
		t.Error("EventsOffered should be initialized")
	}
	if metrics.EventsDropped == nil {
		t.Error("EventsDropped should be initialized")
	}
	if metrics.BufferUtilization == nil {
		t.Error("BufferUtilization should be initialized")
	}

	// Test initial values
	if metrics.TotalSize != 0 {
		t.Errorf("expected TotalSize 0, got %d", metrics.TotalSize)
	}

	// Test that LastUpdateTime is recent
	if time.Since(metrics.LastUpdateTime) > time.Second {
		t.Error("LastUpdateTime should be recent")
	}

	// Test that maps are empty
	if len(metrics.EventsOffered) != 0 {
		t.Error("EventsOffered should be empty initially")
	}
	if len(metrics.EventsDropped) != 0 {
		t.Error("EventsDropped should be empty initially")
	}
	if len(metrics.BufferUtilization) != 0 {
		t.Error("BufferUtilization should be empty initially")
	}
}

func TestTelemetryBufferConfig_DefaultPriorityWeights(t *testing.T) {
	config := DefaultTelemetryBufferConfig()

	// Test that weights are in descending order
	weights := config.PriorityWeights

	if weights[PriorityCritical] <= weights[PriorityHigh] {
		t.Error("critical priority should have higher weight than high")
	}
	if weights[PriorityHigh] <= weights[PriorityMedium] {
		t.Error("high priority should have higher weight than medium")
	}
	if weights[PriorityMedium] <= weights[PriorityLow] {
		t.Error("medium priority should have higher weight than low")
	}
	if weights[PriorityLow] <= weights[PriorityLowest] {
		t.Error("low priority should have higher weight than lowest")
	}
}

func TestTelemetryBufferConfig_DefaultBatchTimeouts(t *testing.T) {
	config := DefaultTelemetryBufferConfig()

	// Critical events (errors, feedback) should have no delay
	if config.BatchTimeouts[DataCategoryError] != 0 {
		t.Error("errors should have no batch timeout")
	}
	if config.BatchTimeouts[DataCategoryFeedback] != 0 {
		t.Error("feedback should have no batch timeout")
	}

	// High volume events (logs, spans) should have longer timeouts
	if config.BatchTimeouts[DataCategoryLog] == 0 {
		t.Error("logs should have batch timeout for efficiency")
	}
	if config.BatchTimeouts[DataCategorySpan] == 0 {
		t.Error("spans should have batch timeout for efficiency")
	}

	// Sessions and check-ins should have short timeouts
	if config.BatchTimeouts[DataCategorySession] == 0 {
		t.Error("sessions should have short batch timeout")
	}
	if config.BatchTimeouts[DataCategoryCheckIn] == 0 {
		t.Error("check-ins should have short batch timeout")
	}
}

func TestTelemetryBufferConfig_DefaultBatchSizes(t *testing.T) {
	config := DefaultTelemetryBufferConfig()

	// Critical events should not be batched
	if config.BatchSizes[DataCategoryError] != 1 {
		t.Error("errors should have batch size 1")
	}
	if config.BatchSizes[DataCategoryFeedback] != 1 {
		t.Error("feedback should have batch size 1")
	}

	// High volume events should have larger batch sizes
	if config.BatchSizes[DataCategoryLog] <= 1 {
		t.Error("logs should have batch size > 1 for efficiency")
	}
	if config.BatchSizes[DataCategorySpan] <= 1 {
		t.Error("spans should have batch size > 1 for efficiency")
	}

	// Batch sizes should not exceed buffer capacities
	for category, batchSize := range config.BatchSizes {
		capacity := config.BufferCapacities[category]
		if batchSize > capacity {
			t.Errorf("batch size %d exceeds capacity %d for category %s",
				batchSize, capacity, category)
		}
	}
}
