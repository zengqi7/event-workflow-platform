package outbox

import "time"

type Event struct {
	ID            uint64
	LeaseToken    string
	TenantID      uint64
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       map[string]any
	Status        string
	RetryCount    int
	NextRetryAt   *time.Time
	PublishedAt   *time.Time
	CreatedAt     time.Time
}

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusPublished  = "published"
	StatusFailed     = "failed"
)
