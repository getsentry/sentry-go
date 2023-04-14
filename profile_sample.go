package sentry

// Based on https://github.com/getsentry/vroom/blob/d11c26063e802d66b9a592c4010261746ca3dfa4/internal/sample/sample.go
// * unused fields are commented out
// * some types are replaced with their representation in sentry-go

import (
	"time"
)

type (
	profileDevice struct {
		Architecture   string `json:"architecture"`
		Classification string `json:"classification"`
		Locale         string `json:"locale"`
		Manufacturer   string `json:"manufacturer"`
		Model          string `json:"model"`
	}

	profileOS struct {
		BuildNumber string `json:"build_number"`
		Name        string `json:"name"`
		Version     string `json:"version"`
	}

	profileRuntime struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	profileSample struct {
		ElapsedSinceStartNS uint64       `json:"elapsed_since_start_ns"`
		QueueAddress        string       `json:"queue_address,omitempty"`
		StackID             int          `json:"stack_id"`
		State               profileState `json:"state,omitempty"`
		ThreadID            uint64       `json:"thread_id"`
	}

	profileThreadMetadata struct {
		Name     string `json:"name,omitempty"`
		Priority int    `json:"priority,omitempty"`
	}

	// QueueMetadata struct {
	// 	Label string `json:"label"`
	// }

	profileStack []int

	profileTrace struct {
		Frames []Frame `json:"frames"`
		// QueueMetadata  map[string]QueueMetadata  `json:"queue_metadata"`
		Samples        []profileSample                  `json:"samples"`
		Stacks         []profileStack                   `json:"stacks"`
		ThreadMetadata map[string]profileThreadMetadata `json:"thread_metadata"`
	}

	profileInfo struct {
		DebugMeta   DebugMeta     `json:"debug_meta"`
		Device      profileDevice `json:"device"`
		Environment string        `json:"environment,omitempty"`
		EventID     string        `json:"event_id"`
		// Measurements   map[string]measurements.Measurement `json:"measurements,omitempty"`
		OS profileOS `json:"os"`
		// OrganizationID uint64                    `json:"organization_id"`
		Platform string `json:"platform"`
		// ProjectID     uint64                    `json:"project_id"`
		// Received      time.Time                 `json:"received"`
		Release string `json:"release"`
		// RetentionDays int                       `json:"retention_days"`
		Runtime     profileRuntime     `json:"runtime"`
		Timestamp   time.Time          `json:"timestamp"`
		Trace       profileTrace       `json:"profile"`
		Transaction profileTransaction `json:"transaction"`
		// Transactions []transaction.Transaction `json:"transactions,omitempty"`
		Version string `json:"version"`
	}

	profileState string

	// see https://github.com/getsentry/vroom/blob/a91e39416723ec44fc54010257020eeaf9a77cbd/internal/transaction/transaction.go
	profileTransaction struct {
		ActiveThreadID uint64 `json:"active_thread_id"`
		DurationNS     uint64 `json:"duration_ns,omitempty"`
		ID             string `json:"id"`
		Name           string `json:"name"`
		TraceID        string `json:"trace_id"`
	}
)
