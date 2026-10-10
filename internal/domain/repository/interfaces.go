package repository

import (
	"context"
	"errors"
	"time"

	domainevent "event-workflow-platform/internal/domain/event"
	"event-workflow-platform/internal/domain/outbox"
	"event-workflow-platform/internal/domain/rule"
	"event-workflow-platform/internal/domain/tenant"
	"event-workflow-platform/internal/domain/workflow"
)

var ErrNotFound = errors.New("not found")
var ErrIdempotencyConflict = errors.New("idempotency conflict")
var ErrInvalidRequest = errors.New("invalid request")
var ErrLeaseLost = errors.New("outbox lease lost")

type Store interface {
	FindTenantByCode(ctx context.Context, code string) (tenant.Tenant, error)
	FindEventByIdempotency(ctx context.Context, tenantID uint64, source, eventID string) (domainevent.Event, error)
	HasProcessedDedup(ctx context.Context, tenantID uint64, dedupKey, dedupBucket string) (bool, error)
	ListRulesByEventType(ctx context.Context, tenantID uint64, eventType string) ([]rule.Rule, error)
	GetWorkflowByID(ctx context.Context, workflowID uint64) (workflow.Workflow, error)
	PersistIngestResult(ctx context.Context, evt domainevent.Event, plans []workflow.RunPlan) (PersistResult, error)
}

type PersistResult struct {
	EventID       uint64
	Status        string
	WorkflowRuns  int
	WorkflowTasks int
}

type Cache interface {
	AcquireIdempotency(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Ping(ctx context.Context) error
}

type WorkerStore interface {
	ClaimPendingOutbox(ctx context.Context, limit int) ([]outbox.Event, error)
	GetTaskByID(ctx context.Context, taskID uint64) (workflow.Task, error)
	FinishDelivery(ctx context.Context, item outbox.Event, task workflow.Task, status, lastError string, nextRetryAt time.Time) error
}

type InspectionStore interface {
	ListRecentEvents(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error)
	ListRecentWorkflowRuns(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error)
	ListRecentWorkflowTasks(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error)
	ListRecentOutboxEvents(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error)
}

type ReadinessStore interface {
	Ping(ctx context.Context) error
}
