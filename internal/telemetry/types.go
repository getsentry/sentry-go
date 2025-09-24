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
	DataCategoryCheckIn     DataCategory = "checkin"
	DataCategoryLog         DataCategory = "log"
)

func (dc DataCategory) String() string {
	return string(dc)
}

func (dc DataCategory) GetPriority() Priority {
	switch dc {
	case DataCategoryError:
		return PriorityCritical
	case DataCategoryCheckIn:
		return PriorityHigh
	case DataCategoryLog:
		return PriorityMedium
	case DataCategoryTransaction:
		return PriorityLow
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
