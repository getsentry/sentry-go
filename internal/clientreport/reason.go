package clientreport

// DiscardReason represents why an item was discarded.
type DiscardReason string

const (
	// ReasonQueueOverflow indicates the transport queue was full.
	ReasonQueueOverflow DiscardReason = "queue_overflow"

	// ReasonBufferOverflow indicates that an internal buffer was full.
	ReasonBufferOverflow DiscardReason = "buffer_overflow"

	// ReasonRateLimitBackoff indicates the item was dropped due to rate limiting.
	ReasonRateLimitBackoff DiscardReason = "ratelimit_backoff"

	// ReasonBeforeSend indicates the item was dropped due to a BeforeSend callback.
	ReasonBeforeSend DiscardReason = "before_send"

	// ReasonEventProcessor indicates the item was dropped due to an event processor callback.
	ReasonEventProcessor DiscardReason = "event_processor"

	// ReasonSampleRate indicates the item was dropped due to sampling.
	ReasonSampleRate DiscardReason = "sample_rate"

	// ReasonNetworkError indicates an HTTP request failed (connection error).
	ReasonNetworkError DiscardReason = "network_error"

	// ReasonSendError indicates HTTP returned an error status (4xx, 5xx).
	ReasonSendError DiscardReason = "send_error"

	// ReasonInternalError indicates an internal SDK error.
	ReasonInternalError DiscardReason = "internal_sdk_error"
)
