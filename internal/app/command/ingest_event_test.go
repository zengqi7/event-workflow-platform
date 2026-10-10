package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"event-workflow-platform/internal/app/dto"
	domainevent "event-workflow-platform/internal/domain/event"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/rule"
	"event-workflow-platform/internal/domain/tenant"
	"event-workflow-platform/internal/domain/workflow"
)

func TestExecuteCreatesWorkflowPlans(t *testing.T) {
	store := &stubStore{
		tenant: tenant.Tenant{ID: 1, Code: "acme", Status: "active"},
		rules: []rule.Rule{
			{
				ID:          10,
				TenantID:    1,
				Name:        "high-value-order-timeout",
				EventType:   "order.timeout",
				Priority:    10,
				Enabled:     true,
				StopOnMatch: true,
				WorkflowID:  20,
				Condition: rule.ConditionGroup{
					Logic: "AND",
					Conditions: []rule.Condition{
						{Field: "event_type", Op: "=", Value: "order.timeout"},
						{Field: "payload.amount", Op: ">", Value: 5000},
					},
				},
			},
		},
		workflow: workflow.Workflow{
			ID:       20,
			TenantID: 1,
			Name:     "timeout",
			Status:   "active",
			Definition: workflow.Definition{
				Nodes: []workflow.Node{
					{Key: "notify_webhook", Type: "webhook", Config: map[string]any{"url": "https://example.com"}},
				},
			},
		},
		persistResult: domainrepo.PersistResult{
			EventID:       101,
			WorkflowRuns:  1,
			WorkflowTasks: 1,
		},
	}

	cache := &stubCache{idempotencyAcquire: true}
	cmd := NewIngestEventCommand(store, cache)

	resp, err := cmd.Execute(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if resp.EventID != 101 {
		t.Fatalf("expected event id 101, got %d", resp.EventID)
	}
	if resp.Status != domainevent.StatusReceived {
		t.Fatalf("expected status received, got %s", resp.Status)
	}
	if resp.WorkflowRuns != 1 || resp.WorkflowTasks != 1 {
		t.Fatalf("expected one run and one task, got runs=%d tasks=%d", resp.WorkflowRuns, resp.WorkflowTasks)
	}
	if len(store.persistPlans) != 1 {
		t.Fatalf("expected one workflow plan, got %d", len(store.persistPlans))
	}
}

func TestExecuteReturnsExistingEventOnIdempotencyHit(t *testing.T) {
	store := &stubStore{
		tenant: tenant.Tenant{ID: 1, Code: "acme", Status: "active"},
		existingEvent: domainevent.Event{
			ID:          999,
			TenantID:    1,
			EventType:   "order.timeout",
			BusinessKey: "order-1001",
			TraceID:     "existing-trace",
			Fingerprint: "existing-fingerprint",
			DedupKey:    "existing-dedup",
			Status:      domainevent.StatusReceived,
			Payload:     map[string]any{"amount": 6999.0},
			OccurredAt:  time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC),
		},
	}
	cache := &stubCache{idempotencyAcquire: false}
	cmd := NewIngestEventCommand(store, cache)

	resp, err := cmd.Execute(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if resp.EventID != 999 {
		t.Fatalf("expected existing event id 999, got %d", resp.EventID)
	}
	if store.persistCalls != 0 {
		t.Fatalf("expected no persist call, got %d", store.persistCalls)
	}
}

func TestExecuteRejectsIdempotencyKeyWithDifferentPayload(t *testing.T) {
	store := &stubStore{
		tenant: tenant.Tenant{ID: 1, Code: "acme", Status: "active"},
		existingEvent: domainevent.Event{
			ID: 999, TenantID: 1, EventType: "order.timeout", BusinessKey: "order-1001",
			Payload: map[string]any{"amount": 1.0}, OccurredAt: time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC),
		},
	}
	cmd := NewIngestEventCommand(store, &stubCache{idempotencyAcquire: false})

	if _, err := cmd.Execute(context.Background(), testRequest()); !errors.Is(err, domainrepo.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestExecuteSkipsWorkflowOnDedupHit(t *testing.T) {
	store := &stubStore{
		tenant:        tenant.Tenant{ID: 1, Code: "acme", Status: "active"},
		hasDedup:      true,
		persistResult: domainrepo.PersistResult{EventID: 201},
	}
	cache := &stubCache{idempotencyAcquire: true}
	cmd := NewIngestEventCommand(store, cache)

	resp, err := cmd.Execute(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if resp.Status != domainevent.StatusDedupSkipped {
		t.Fatalf("expected dedup skipped, got %s", resp.Status)
	}
	if len(store.persistPlans) != 0 {
		t.Fatalf("expected no workflow plans persisted, got %d", len(store.persistPlans))
	}
}

func testRequest() dto.IngestEventRequest {
	return dto.IngestEventRequest{
		TenantCode:  "acme",
		Source:      "order-service",
		EventType:   "order.timeout",
		EventID:     "evt-001",
		BusinessKey: "order-1001",
		OccurredAt:  time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC).Format(time.RFC3339),
		Payload: map[string]any{
			"amount": 6999.0,
		},
	}
}

type stubStore struct {
	tenant        tenant.Tenant
	existingEvent domainevent.Event
	hasDedup      bool
	rules         []rule.Rule
	workflow      workflow.Workflow
	persistPlans  []workflow.RunPlan
	persistCalls  int
	persistResult domainrepo.PersistResult
}

func (s *stubStore) FindTenantByCode(context.Context, string) (tenant.Tenant, error) {
	return s.tenant, nil
}

func (s *stubStore) FindEventByIdempotency(context.Context, uint64, string, string) (domainevent.Event, error) {
	if s.existingEvent.ID == 0 {
		return domainevent.Event{}, domainrepo.ErrNotFound
	}
	return s.existingEvent, nil
}

func (s *stubStore) HasProcessedDedup(context.Context, uint64, string, string) (bool, error) {
	return s.hasDedup, nil
}

func (s *stubStore) ListRulesByEventType(context.Context, uint64, string) ([]rule.Rule, error) {
	return s.rules, nil
}

func (s *stubStore) GetWorkflowByID(context.Context, uint64) (workflow.Workflow, error) {
	if s.workflow.ID == 0 {
		return workflow.Workflow{}, domainrepo.ErrNotFound
	}
	return s.workflow, nil
}

func (s *stubStore) PersistIngestResult(_ context.Context, _ domainevent.Event, plans []workflow.RunPlan) (domainrepo.PersistResult, error) {
	s.persistCalls++
	s.persistPlans = plans
	return s.persistResult, nil
}

type stubCache struct {
	idempotencyAcquire bool
}

func (s *stubCache) AcquireIdempotency(context.Context, string, time.Duration) (bool, error) {
	return s.idempotencyAcquire, nil
}

func (s *stubCache) Ping(context.Context) error { return nil }
