package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"event-workflow-platform/internal/app/dto"
	domainevent "event-workflow-platform/internal/domain/event"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/infra"
	"event-workflow-platform/internal/service/dedup"
	"event-workflow-platform/internal/service/ruleeval"
	"event-workflow-platform/internal/service/workflowengine"
)

type IngestEventCommand struct {
	store domainrepo.Store
	cache domainrepo.Cache
}

func NewIngestEventCommand(store domainrepo.Store, cache domainrepo.Cache) *IngestEventCommand {
	return &IngestEventCommand{
		store: store,
		cache: cache,
	}
}

func (c *IngestEventCommand) Execute(ctx context.Context, req dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	if err := validateIngestEventRequest(req); err != nil {
		return dto.IngestEventResponse{}, fmt.Errorf("%w: %v", domainrepo.ErrInvalidRequest, err)
	}

	tenantCode := strings.ToLower(strings.TrimSpace(req.TenantCode))
	tenantRecord, err := c.store.FindTenantByCode(ctx, tenantCode)
	if err != nil {
		if errors.Is(err, domainrepo.ErrNotFound) {
			return dto.IngestEventResponse{}, fmt.Errorf("tenant %q: %w", req.TenantCode, domainrepo.ErrNotFound)
		}
		return dto.IngestEventResponse{}, err
	}

	source := strings.TrimSpace(req.Source)
	eventID := strings.TrimSpace(req.EventID)
	eventType := strings.TrimSpace(req.EventType)
	businessKey := strings.TrimSpace(req.BusinessKey)

	occurredAt, err := time.Parse(time.RFC3339, req.OccurredAt)
	if err != nil {
		return dto.IngestEventResponse{}, fmt.Errorf("%w: invalid occurred_at", domainrepo.ErrInvalidRequest)
	}
	occurredAt = occurredAt.UTC()

	if response, handled, err := c.tryReuseExisting(ctx, tenantRecord.ID, source, eventID, eventType, businessKey, occurredAt, req.Payload); handled {
		return response, err
	}

	traceID := infra.NewTraceID()
	fingerprint := dedup.Fingerprint(tenantRecord.ID, source, eventType, eventID, businessKey)
	dedupKey := dedup.DedupKey(tenantRecord.ID, eventType, businessKey)
	if businessKey == "" {
		// Without a business key only transport idempotency should suppress events.
		dedupKey = fingerprint
	}
	dedupBucket := dedup.DedupBucket(occurredAt)

	evt := domainevent.Event{
		TenantID:    tenantRecord.ID,
		Source:      source,
		EventType:   eventType,
		EventID:     eventID,
		BusinessKey: businessKey,
		TraceID:     traceID,
		Fingerprint: fingerprint,
		DedupKey:    dedupKey,
		DedupBucket: dedupBucket,
		Status:      domainevent.StatusReceived,
		Payload:     req.Payload,
		OccurredAt:  occurredAt,
		CreatedAt:   time.Now().UTC(),
	}

	isDedupHit, err := c.isDedupHit(ctx, evt)
	if err != nil {
		return dto.IngestEventResponse{}, err
	}

	var plans []workflow.RunPlan
	if isDedupHit {
		evt.Status = domainevent.StatusDedupSkipped
	} else {
		plans, err = c.buildRunPlans(ctx, evt)
		if err != nil {
			return dto.IngestEventResponse{}, err
		}
	}

	persisted, err := c.store.PersistIngestResult(ctx, evt, plans)
	if err != nil {
		if errors.Is(err, domainrepo.ErrIdempotencyConflict) {
			existing, findErr := c.store.FindEventByIdempotency(ctx, evt.TenantID, source, eventID)
			if findErr == nil {
				if !sameIdempotentRequest(existing, eventType, businessKey, occurredAt, req.Payload) {
					return dto.IngestEventResponse{}, domainrepo.ErrIdempotencyConflict
				}
				infra.IncIdempotencyHit()
				return buildExistingResponse(existing), nil
			}
			return dto.IngestEventResponse{}, findErr
		}
		return dto.IngestEventResponse{}, err
	}
	if persisted.Status != "" {
		evt.Status = persisted.Status
	}

	infra.IncEventIngest()
	if evt.Status == domainevent.StatusDedupSkipped {
		infra.IncDedupSkipped()
	}
	infra.AddWorkflowRuns(persisted.WorkflowRuns)
	infra.AddWorkflowTasks(persisted.WorkflowTasks)

	return dto.IngestEventResponse{
		EventID:       persisted.EventID,
		TenantID:      evt.TenantID,
		TraceID:       evt.TraceID,
		Fingerprint:   evt.Fingerprint,
		DedupKey:      evt.DedupKey,
		Status:        evt.Status,
		WorkflowRuns:  persisted.WorkflowRuns,
		WorkflowTasks: persisted.WorkflowTasks,
	}, nil
}

func validateIngestEventRequest(req dto.IngestEventRequest) error {
	switch {
	case strings.TrimSpace(req.TenantCode) == "":
		return errors.New("tenant_code is required")
	case len(strings.TrimSpace(req.TenantCode)) > 64:
		return errors.New("tenant_code exceeds 64 bytes")
	case strings.TrimSpace(req.Source) == "":
		return errors.New("source is required")
	case len(strings.TrimSpace(req.Source)) > 64:
		return errors.New("source exceeds 64 bytes")
	case strings.TrimSpace(req.EventType) == "":
		return errors.New("event_type is required")
	case len(strings.TrimSpace(req.EventType)) > 128:
		return errors.New("event_type exceeds 128 bytes")
	case strings.TrimSpace(req.EventID) == "":
		return errors.New("event_id is required")
	case len(strings.TrimSpace(req.EventID)) > 128:
		return errors.New("event_id exceeds 128 bytes")
	case len(strings.TrimSpace(req.BusinessKey)) > 128:
		return errors.New("business_key exceeds 128 bytes")
	case strings.TrimSpace(req.OccurredAt) == "":
		return errors.New("occurred_at is required")
	case req.Payload == nil:
		return errors.New("payload is required")
	default:
		return nil
	}
}

func (c *IngestEventCommand) tryReuseExisting(ctx context.Context, tenantID uint64, source, eventID, eventType, businessKey string, occurredAt time.Time, payload map[string]any) (dto.IngestEventResponse, bool, error) {
	idempotencyKey := fmt.Sprintf("idempotency:%d:%s:%s", tenantID, source, eventID)
	if c.cache != nil {
		acquired, err := c.cache.AcquireIdempotency(ctx, idempotencyKey, 10*time.Minute)
		if err == nil && !acquired {
			existing, findErr := c.store.FindEventByIdempotency(ctx, tenantID, source, eventID)
			if findErr == nil {
				if !sameIdempotentRequest(existing, eventType, businessKey, occurredAt, payload) {
					return dto.IngestEventResponse{}, true, domainrepo.ErrIdempotencyConflict
				}
				infra.IncIdempotencyHit()
				return buildExistingResponse(existing), true, nil
			}
		}
	}

	existing, err := c.store.FindEventByIdempotency(ctx, tenantID, source, eventID)
	if err == nil {
		if !sameIdempotentRequest(existing, eventType, businessKey, occurredAt, payload) {
			return dto.IngestEventResponse{}, true, domainrepo.ErrIdempotencyConflict
		}
		infra.IncIdempotencyHit()
		return buildExistingResponse(existing), true, nil
	}
	if !errors.Is(err, domainrepo.ErrNotFound) {
		return dto.IngestEventResponse{}, true, err
	}

	return dto.IngestEventResponse{}, false, nil
}

func sameIdempotentRequest(existing domainevent.Event, eventType, businessKey string, occurredAt time.Time, payload map[string]any) bool {
	if existing.EventType != eventType || existing.BusinessKey != businessKey || !existing.OccurredAt.Equal(occurredAt) {
		return false
	}
	existingPayload, existingErr := json.Marshal(existing.Payload)
	requestPayload, requestErr := json.Marshal(payload)
	return existingErr == nil && requestErr == nil && bytes.Equal(existingPayload, requestPayload)
}

func (c *IngestEventCommand) isDedupHit(ctx context.Context, evt domainevent.Event) (bool, error) {
	// A Redis reservation is not evidence that the database transaction committed.
	// The unique claim inside PersistIngestResult resolves concurrent arrivals.
	return c.store.HasProcessedDedup(ctx, evt.TenantID, evt.DedupKey, evt.DedupBucket)
}

func (c *IngestEventCommand) buildRunPlans(ctx context.Context, evt domainevent.Event) ([]workflow.RunPlan, error) {
	rules, err := c.store.ListRulesByEventType(ctx, evt.TenantID, evt.EventType)
	if err != nil {
		return nil, err
	}

	plans := make([]workflow.RunPlan, 0)
	for _, matchedRule := range rules {
		if !ruleeval.Match(evt, matchedRule.Condition) {
			continue
		}
		if matchedRule.WorkflowID == 0 {
			continue
		}

		workflowDefinition, err := c.store.GetWorkflowByID(ctx, matchedRule.WorkflowID)
		if err != nil {
			if errors.Is(err, domainrepo.ErrNotFound) {
				continue
			}
			return nil, err
		}

		if workflowDefinition.TenantID != evt.TenantID || workflowDefinition.Status != "active" {
			continue
		}
		plans = append(plans, workflowengine.BuildRunPlan(evt, matchedRule, workflowDefinition))
		if matchedRule.StopOnMatch {
			break
		}
	}

	return plans, nil
}

func buildExistingResponse(existing domainevent.Event) dto.IngestEventResponse {
	return dto.IngestEventResponse{
		EventID:       existing.ID,
		TenantID:      existing.TenantID,
		TraceID:       existing.TraceID,
		Fingerprint:   existing.Fingerprint,
		DedupKey:      existing.DedupKey,
		Status:        existing.Status,
		WorkflowRuns:  0,
		WorkflowTasks: 0,
	}
}
