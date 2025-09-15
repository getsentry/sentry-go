package telemetry

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
