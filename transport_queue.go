package sentry

type Priority int

const (
	HighPriority Priority = iota
	MediumPriority
	LowPriority
)

func getEventPriority(event *Event) Priority {
	switch eventToBuffer(event.Type) {
	case ErrorBuffer:
		return HighPriority
	case TransactionBuffer:
		return MediumPriority
	case LogBuffer:
		return LowPriority
	default:
		return 4
	}
}

// transportQueue implements heap.Interface for Events.
type transportQueue []*Event

func (q *transportQueue) Len() int { return len(*q) }

func (q *transportQueue) Less(i, j int) bool {
	return getEventPriority((*q)[i]) < getEventPriority((*q)[j])
}

func (q *transportQueue) Swap(i, j int) {
	(*q)[i], (*q)[j] = (*q)[j], (*q)[i]
}

func (q *transportQueue) Push(x interface{}) {
	item := x.(*Event)
	*q = append(*q, item)
}

func (q *transportQueue) Pop() interface{} {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*q = old[0 : n-1]
	return item
}
