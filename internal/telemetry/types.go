package telemetry

import "time"

type Priority int

const (
	PriorityCritical Priority = iota + 1
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityLowest
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	case PriorityLowest:
		return "lowest"
	default:
		return "unknown"
	}
}

type DataCategory string

const (
	DataCategoryError       DataCategory = "error"
	DataCategoryTransaction DataCategory = "transaction"
	DataCategorySession     DataCategory = "session"
	DataCategoryCheckIn     DataCategory = "checkin"
	DataCategoryLog         DataCategory = "log"
	DataCategorySpan        DataCategory = "span"
	DataCategoryProfile     DataCategory = "profile"
	DataCategoryReplay      DataCategory = "replay"
	DataCategoryFeedback    DataCategory = "feedback"
)

func (dc DataCategory) String() string {
	return string(dc)
}

func (dc DataCategory) GetPriority() Priority {
	switch dc {
	case DataCategoryError, DataCategoryFeedback:
		return PriorityCritical
	case DataCategorySession, DataCategoryCheckIn:
		return PriorityHigh
	case DataCategoryLog, DataCategorySpan:
		return PriorityMedium
	case DataCategoryTransaction, DataCategoryProfile:
		return PriorityLow
	case DataCategoryReplay:
		return PriorityLowest
	default:
		return PriorityMedium
	}
}

// OverflowPolicy defines how the ring buffer handles overflow
type OverflowPolicy int

const (
	OverflowPolicyDropOldest OverflowPolicy = iota
	OverflowPolicyDropNewest
)

func (op OverflowPolicy) String() string {
	switch op {
	case OverflowPolicyDropOldest:
		return "drop_oldest"
	case OverflowPolicyDropNewest:
		return "drop_newest"
	default:
		return "unknown"
	}
}

type BufferConfig struct {
	// Enabled determines if the telemetry buffer system is active
	Enabled bool

	// BufferCapacities defines the capacity for each data category buffer
	BufferCapacities map[DataCategory]int

	// PriorityWeights defines the weight for each priority level in the scheduler
	PriorityWeights map[Priority]int

	// BatchSizes defines the batch size for each data category
	BatchSizes map[DataCategory]int

	// BatchTimeouts defines the maximum time to wait before sending a batch
	BatchTimeouts map[DataCategory]time.Duration

	// OverflowPolicies defines how each buffer handles overflow
	OverflowPolicies map[DataCategory]OverflowPolicy
}

func DefaultTelemetryBufferConfig() *BufferConfig {
	return &BufferConfig{
		Enabled: false, // Feature flag for gradual rollout
		BufferCapacities: map[DataCategory]int{
			DataCategoryError:       100,  // Small buffer for errors
			DataCategoryTransaction: 500,  // Medium buffer for transactions
			DataCategorySession:     100,  // Small buffer for sessions
			DataCategoryCheckIn:     50,   // Small buffer for check-ins
			DataCategoryLog:         1000, // Large buffer for logs
			DataCategorySpan:        1000, // Large buffer for spans
			DataCategoryProfile:     100,  // Small buffer for profiles
			DataCategoryReplay:      10,   // Very small for replays
			DataCategoryFeedback:    50,   // Small buffer for feedback
		},
		PriorityWeights: map[Priority]int{
			PriorityCritical: 5, // Errors get 5x weight
			PriorityHigh:     4, // Sessions get 4x weight
			PriorityMedium:   3, // Logs get 3x weight
			PriorityLow:      2, // Transactions get 2x weight
			PriorityLowest:   1, // Replays get 1x weight
		},
		BatchSizes: map[DataCategory]int{
			DataCategoryError:       1,  // Send errors immediately
			DataCategoryTransaction: 1,  // Send transactions individually
			DataCategorySession:     5,  // Small batches for sessions
			DataCategoryCheckIn:     5,  // Small batches for check-ins
			DataCategoryLog:         50, // Batch logs for efficiency
			DataCategorySpan:        50, // Batch spans for efficiency
			DataCategoryProfile:     1,  // Send profiles individually
			DataCategoryReplay:      1,  // Send replays individually
			DataCategoryFeedback:    1,  // Send feedback immediately
		},
		BatchTimeouts: map[DataCategory]time.Duration{
			DataCategoryError:       0,               // No batching delay
			DataCategoryTransaction: 0,               // No batching delay
			DataCategorySession:     1 * time.Second, // 1s batching window
			DataCategoryCheckIn:     1 * time.Second, // 1s batching window
			DataCategoryLog:         5 * time.Second, // 5s batching window
			DataCategorySpan:        5 * time.Second, // 5s batching window
			DataCategoryProfile:     0,               // No batching delay
			DataCategoryReplay:      0,               // No batching delay
			DataCategoryFeedback:    0,               // No batching delay
		},
		OverflowPolicies: map[DataCategory]OverflowPolicy{
			DataCategoryError:       OverflowPolicyDropOldest, // Keep newest errors
			DataCategoryTransaction: OverflowPolicyDropOldest, // Keep newest transactions
			DataCategorySession:     OverflowPolicyDropOldest, // Keep newest sessions
			DataCategoryCheckIn:     OverflowPolicyDropOldest, // Keep newest check-ins
			DataCategoryLog:         OverflowPolicyDropOldest, // Keep newest logs
			DataCategorySpan:        OverflowPolicyDropOldest, // Keep newest spans
			DataCategoryProfile:     OverflowPolicyDropOldest, // Keep newest profiles
			DataCategoryReplay:      OverflowPolicyDropOldest, // Keep newest replays
			DataCategoryFeedback:    OverflowPolicyDropOldest, // Keep newest feedback
		},
	}
}

func (config *BufferConfig) Validate() error {
	if config.BufferCapacities == nil {
		config.BufferCapacities = DefaultTelemetryBufferConfig().BufferCapacities
	}
	if config.PriorityWeights == nil {
		config.PriorityWeights = DefaultTelemetryBufferConfig().PriorityWeights
	}
	if config.BatchSizes == nil {
		config.BatchSizes = DefaultTelemetryBufferConfig().BatchSizes
	}
	if config.BatchTimeouts == nil {
		config.BatchTimeouts = DefaultTelemetryBufferConfig().BatchTimeouts
	}
	if config.OverflowPolicies == nil {
		config.OverflowPolicies = DefaultTelemetryBufferConfig().OverflowPolicies
	}

	for category, capacity := range config.BufferCapacities {
		if capacity <= 0 {
			return &ConfigValidationError{
				Field:   "BufferCapacities",
				Message: "capacity must be positive for category " + string(category),
			}
		}
	}

	for priority, weight := range config.PriorityWeights {
		if weight <= 0 {
			return &ConfigValidationError{
				Field:   "PriorityWeights",
				Message: "weight must be positive for priority " + priority.String(),
			}
		}
	}

	for category, batchSize := range config.BatchSizes {
		if capacity, exists := config.BufferCapacities[category]; exists {
			if batchSize > capacity {
				return &ConfigValidationError{
					Field:   "BatchSizes",
					Message: "batch size cannot exceed buffer capacity for category " + string(category),
				}
			}
		}
	}

	return nil
}

type ConfigValidationError struct {
	Field   string
	Message string
}

func (e *ConfigValidationError) Error() string {
	return "telemetry config validation error in " + e.Field + ": " + e.Message
}

type Metrics struct {
	// EventsOffered tracks the number of events offered to each category buffer
	EventsOffered map[DataCategory]int64

	// EventsDropped tracks the number of events dropped by category and reason
	EventsDropped map[DataCategory]map[string]int64

	// BufferUtilization tracks the current utilization percentage for each buffer
	BufferUtilization map[DataCategory]float64

	// TotalSize tracks the current total size across all buffers
	TotalSize int64

	// LastUpdateTime tracks when metrics were last updated
	LastUpdateTime time.Time
}

func NewTelemetryMetrics() *Metrics {
	return &Metrics{
		EventsOffered:     make(map[DataCategory]int64),
		EventsDropped:     make(map[DataCategory]map[string]int64),
		BufferUtilization: make(map[DataCategory]float64),
		TotalSize:         0,
		LastUpdateTime:    time.Now(),
	}
}
