package telemetry

// OverflowPolicy defines how the ring buffer handles overflow.
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
