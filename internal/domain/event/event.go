package event

import "time"

type Event struct {
	ID          uint64
	TenantID    uint64
	Source      string
	EventType   string
	EventID     string
	BusinessKey string
	TraceID     string
	Fingerprint string
	DedupKey    string
	DedupBucket string
	Status      string
	Payload     map[string]any
	OccurredAt  time.Time
	CreatedAt   time.Time
}

const (
	StatusReceived     = "received"
	StatusDedupSkipped = "dedup_skipped"
)
