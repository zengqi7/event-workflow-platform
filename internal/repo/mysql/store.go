package mysqlrepo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqlerr "github.com/go-sql-driver/mysql"

	domainevent "event-workflow-platform/internal/domain/event"
	"event-workflow-platform/internal/domain/outbox"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/rule"
	"event-workflow-platform/internal/domain/tenant"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/infra"
	"event-workflow-platform/internal/service/auth"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) FindTenantByCode(ctx context.Context, code string) (tenant.Tenant, error) {
	const query = `
SELECT id, name, code, status
FROM tenants
WHERE code = ? AND status = 'active'
LIMIT 1`

	var result tenant.Tenant
	err := s.db.QueryRowContext(ctx, query, code).Scan(&result.ID, &result.Name, &result.Code, &result.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.Tenant{}, domainrepo.ErrNotFound
	}
	return result, err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) FindEventByIdempotency(ctx context.Context, tenantID uint64, source, eventID string) (domainevent.Event, error) {
	const query = `
SELECT id, tenant_id, source, event_type, event_id, COALESCE(business_key, ''), COALESCE(trace_id, ''), fingerprint, dedup_key, dedup_bucket, status, payload_json, occurred_at, created_at
FROM events
WHERE tenant_id = ? AND source = ? AND event_id = ?
LIMIT 1`

	var (
		evt        domainevent.Event
		payloadRaw []byte
	)
	err := s.db.QueryRowContext(ctx, query, tenantID, source, eventID).Scan(
		&evt.ID,
		&evt.TenantID,
		&evt.Source,
		&evt.EventType,
		&evt.EventID,
		&evt.BusinessKey,
		&evt.TraceID,
		&evt.Fingerprint,
		&evt.DedupKey,
		&evt.DedupBucket,
		&evt.Status,
		&payloadRaw,
		&evt.OccurredAt,
		&evt.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domainevent.Event{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return domainevent.Event{}, err
	}
	if err := json.Unmarshal(payloadRaw, &evt.Payload); err != nil {
		return domainevent.Event{}, err
	}
	return evt, nil
}

func (s *Store) HasProcessedDedup(ctx context.Context, tenantID uint64, dedupKey, dedupBucket string) (bool, error) {
	const query = `
SELECT EXISTS(
  SELECT 1
  FROM events
  WHERE tenant_id = ? AND dedup_key = ? AND dedup_bucket = ? AND status = 'received'
)`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, tenantID, dedupKey, dedupBucket).Scan(&exists)
	return exists, err
}

func (s *Store) ListRulesByEventType(ctx context.Context, tenantID uint64, eventType string) ([]rule.Rule, error) {
	const query = `
SELECT id, tenant_id, name, event_type, priority, enabled, stop_on_match, condition_json, COALESCE(workflow_id, 0)
FROM rules
WHERE tenant_id = ? AND event_type = ? AND enabled = 1
ORDER BY priority ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, tenantID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]rule.Rule, 0)
	for rows.Next() {
		var (
			item         rule.Rule
			conditionRaw []byte
		)
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.Name,
			&item.EventType,
			&item.Priority,
			&item.Enabled,
			&item.StopOnMatch,
			&conditionRaw,
			&item.WorkflowID,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditionRaw, &item.Condition); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, rows.Err()
}

func (s *Store) GetWorkflowByID(ctx context.Context, workflowID uint64) (workflow.Workflow, error) {
	const query = `
SELECT id, tenant_id, name, version, status, definition_json
FROM workflows
WHERE id = ?
LIMIT 1`

	var (
		item          workflow.Workflow
		definitionRaw []byte
	)
	err := s.db.QueryRowContext(ctx, query, workflowID).Scan(
		&item.ID,
		&item.TenantID,
		&item.Name,
		&item.Version,
		&item.Status,
		&definitionRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.Workflow{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return workflow.Workflow{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(definitionRaw))
	decoder.UseNumber()
	if err := decoder.Decode(&item.Definition); err != nil {
		return workflow.Workflow{}, err
	}
	return item, nil
}

func (s *Store) PersistIngestResult(ctx context.Context, evt domainevent.Event, plans []workflow.RunPlan) (domainrepo.PersistResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domainrepo.PersistResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var maxPending int
	if err := tx.QueryRowContext(ctx, `SELECT max_pending_tasks FROM tenants WHERE id=? FOR UPDATE`, evt.TenantID).Scan(&maxPending); err != nil {
		return domainrepo.PersistResult{}, err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_tasks t JOIN workflow_runs r ON r.id=t.run_id WHERE r.tenant_id=? AND t.status IN ('pending','blocked','running','retrying')`, evt.TenantID).Scan(&pending); err != nil {
		return domainrepo.PersistResult{}, err
	}
	newTasks := 0
	for _, plan := range plans {
		newTasks += len(plan.Tasks)
	}
	if pending+newTasks > maxPending {
		return domainrepo.PersistResult{}, auth.ErrQuota
	}

	payloadRaw, err := json.Marshal(evt.Payload)
	if err != nil {
		return domainrepo.PersistResult{}, err
	}

	const insertEventQuery = `
INSERT INTO events (
  tenant_id, source, event_type, event_id, business_key, trace_id, fingerprint, dedup_key, dedup_bucket, status, payload_json, occurred_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	eventResult, err := tx.ExecContext(ctx, insertEventQuery,
		evt.TenantID,
		evt.Source,
		evt.EventType,
		evt.EventID,
		nullIfEmpty(evt.BusinessKey),
		nullIfEmpty(evt.TraceID),
		evt.Fingerprint,
		evt.DedupKey,
		evt.DedupBucket,
		evt.Status,
		payloadRaw,
		evt.OccurredAt,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return domainrepo.PersistResult{}, domainrepo.ErrIdempotencyConflict
		}
		return domainrepo.PersistResult{}, err
	}

	eventID, err := eventResult.LastInsertId()
	if err != nil {
		return domainrepo.PersistResult{}, err
	}
	if evt.Status == domainevent.StatusReceived {
		_, claimErr := tx.ExecContext(ctx, `INSERT INTO event_dedup_claims (tenant_id, dedup_key, dedup_bucket, event_id) VALUES (?, ?, ?, ?)`, evt.TenantID, evt.DedupKey, evt.DedupBucket, eventID)
		if isDuplicateKey(claimErr) {
			evt.Status = domainevent.StatusDedupSkipped
			plans = nil
			if _, err := tx.ExecContext(ctx, `UPDATE events SET status = ? WHERE id = ?`, evt.Status, eventID); err != nil {
				return domainrepo.PersistResult{}, err
			}
		} else if claimErr != nil {
			return domainrepo.PersistResult{}, claimErr
		}
	}

	result := domainrepo.PersistResult{
		EventID: uint64(eventID),
		Status:  evt.Status,
	}

	const insertRunQuery = `
INSERT INTO workflow_runs (
  tenant_id, event_id, workflow_id, trigger_rule_id, status, started_at, workflow_version, definition_json, definition_digest, snapshot_origin
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'runtime')`

	const insertTaskQuery = `
INSERT INTO workflow_tasks (
  run_id, node_key, task_type, status, retry_count, max_retry, scheduled_at, idempotency_key
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	const insertOutboxQuery = `
INSERT INTO outbox_events (
  tenant_id, aggregate_type, aggregate_id, event_type, payload_json, status
) VALUES (?, ?, ?, ?, ?, ?)`

	for _, plan := range plans {
		runResult, err := tx.ExecContext(ctx, insertRunQuery,
			evt.TenantID,
			eventID,
			plan.WorkflowID,
			plan.TriggerRuleID,
			"pending",
			evt.CreatedAt,
			plan.WorkflowVersion,
			plan.DefinitionJSON,
			plan.DefinitionDigest,
		)
		if err != nil {
			return domainrepo.PersistResult{}, err
		}

		runID, err := runResult.LastInsertId()
		if err != nil {
			return domainrepo.PersistResult{}, err
		}
		result.WorkflowRuns++

		for _, task := range plan.Tasks {
			taskResult, err := tx.ExecContext(ctx, insertTaskQuery,
				runID,
				task.NodeKey,
				task.TaskType,
				"pending",
				0,
				3,
				task.ScheduledAt,
				task.IdempotencyKey,
			)
			if err != nil {
				return domainrepo.PersistResult{}, err
			}

			taskID, err := taskResult.LastInsertId()
			if err != nil {
				return domainrepo.PersistResult{}, err
			}

			outboxPayload, err := json.Marshal(map[string]any{
				"tenant_id":       evt.TenantID,
				"event_id":        eventID,
				"run_id":          runID,
				"task_id":         taskID,
				"task_type":       task.TaskType,
				"config":          task.Config,
				"trace_id":        evt.TraceID,
				"payload":         evt.Payload,
				"source_event_id": evt.EventID,
				"event_type":      evt.EventType,
			})
			if err != nil {
				return domainrepo.PersistResult{}, err
			}

			if _, err := tx.ExecContext(ctx, insertOutboxQuery,
				evt.TenantID,
				"workflow_task",
				fmt.Sprintf("%d", taskID),
				"workflow.task.created",
				outboxPayload,
				"pending",
			); err != nil {
				return domainrepo.PersistResult{}, err
			}

			result.WorkflowTasks++
		}
	}

	if err := tx.Commit(); err != nil {
		return domainrepo.PersistResult{}, err
	}

	return result, nil
}

func isDuplicateKey(err error) bool {
	var mysqlError *mysqlerr.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) ClaimPendingOutbox(ctx context.Context, limit int) ([]outbox.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const selectQuery = `
SELECT id, tenant_id, aggregate_type, aggregate_id, event_type, payload_json, status, retry_count, next_retry_at, published_at, created_at
FROM outbox_events
WHERE (status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= UTC_TIMESTAMP(3)))
   OR (status = 'processing' AND lease_until <= UTC_TIMESTAMP(3))
ORDER BY id
LIMIT ?
FOR UPDATE SKIP LOCKED`

	rows, err := tx.QueryContext(ctx, selectQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]outbox.Event, 0)
	for rows.Next() {
		var (
			item       outbox.Event
			payloadRaw []byte
		)
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.AggregateType,
			&item.AggregateID,
			&item.EventType,
			&payloadRaw,
			&item.Status,
			&item.RetryCount,
			&item.NextRetryAt,
			&item.PublishedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(payloadRaw))
		decoder.UseNumber()
		if err := decoder.Decode(&item.Payload); err != nil {
			return nil, err
		}
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return events, nil
	}

	const updateQuery = `
UPDATE outbox_events
SET status = 'processing', lease_token = ?, lease_until = DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 60 SECOND), updated_at = UTC_TIMESTAMP(3)
WHERE id = ?`

	for i := range events {
		item := &events[i]
		item.LeaseToken = infra.NewTraceID()
		if _, err := tx.ExecContext(ctx, updateQuery, item.LeaseToken, item.ID); err != nil {
			return nil, err
		}
		if item.AggregateType == "workflow_task" {
			if _, err := tx.ExecContext(ctx, `
UPDATE workflow_tasks
SET status = 'running', started_at = COALESCE(started_at, UTC_TIMESTAMP(3)), updated_at = UTC_TIMESTAMP(3)
WHERE id = CAST(? AS UNSIGNED) AND status IN ('pending', 'retrying', 'running')`, item.AggregateID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE workflow_runs
SET status = 'running', updated_at = UTC_TIMESTAMP(3)
WHERE id = (SELECT run_id FROM workflow_tasks WHERE id = CAST(? AS UNSIGNED))
  AND status IN ('pending', 'running')`, item.AggregateID); err != nil {
				return nil, err
			}
		}
		item.Status = outbox.StatusProcessing
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

// FinishDelivery fences stale workers and commits task/outbox/run state together.
func (s *Store) FinishDelivery(ctx context.Context, item outbox.Event, task workflow.Task, status, lastError string, nextRetryAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var token, current string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(lease_token, ''), status FROM outbox_events WHERE id = ? FOR UPDATE`, item.ID).Scan(&token, &current); err != nil {
		return err
	}
	if current != "processing" || token != item.LeaseToken {
		return domainrepo.ErrLeaseLost
	}
	outboxStatus := "published"
	var retryAt any
	if status == "retrying" {
		outboxStatus = "pending"
		retryAt = nextRetryAt
	}
	if status == "failed" {
		outboxStatus = "failed"
	}
	if status != "success" && status != "retrying" && status != "failed" {
		return fmt.Errorf("invalid delivery status %q", status)
	}
	if task.ID != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_tasks SET status = ?, last_error = ?, retry_count = retry_count + IF(? = 'success', 0, 1), scheduled_at = COALESCE(?, scheduled_at), started_at = COALESCE(started_at, UTC_TIMESTAMP(3)), finished_at = IF(? IN ('success', 'failed'), UTC_TIMESTAMP(3), NULL) WHERE id = ? AND run_id = ?`, status, lastError, status, retryAt, status, task.ID, task.RunID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status = ?, retry_count = retry_count + IF(? = 'published', 0, 1), next_retry_at = ?, published_at = IF(? = 'published', UTC_TIMESTAMP(3), NULL), lease_token = NULL, lease_until = NULL WHERE id = ?`, outboxStatus, outboxStatus, retryAt, outboxStatus, item.ID); err != nil {
		return err
	}
	if task.RunID != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status = CASE WHEN EXISTS (SELECT 1 FROM workflow_tasks WHERE run_id = ? AND status = 'failed') THEN 'failed' WHEN EXISTS (SELECT 1 FROM workflow_tasks WHERE run_id = ? AND status IN ('pending','running','retrying')) THEN 'running' ELSE 'success' END, finished_at = IF(EXISTS (SELECT 1 FROM workflow_tasks WHERE run_id = ? AND status IN ('pending','running','retrying')), NULL, UTC_TIMESTAMP(3)) WHERE id = ?`, task.RunID, task.RunID, task.RunID, task.RunID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetTaskByID(ctx context.Context, taskID uint64) (workflow.Task, error) {
	const query = `
SELECT id, run_id, node_key, task_type, status, retry_count, max_retry, COALESCE(last_error, ''), scheduled_at, started_at, finished_at, idempotency_key
FROM workflow_tasks
WHERE id = ?
LIMIT 1`

	var task workflow.Task
	err := s.db.QueryRowContext(ctx, query, taskID).Scan(
		&task.ID,
		&task.RunID,
		&task.NodeKey,
		&task.TaskType,
		&task.Status,
		&task.RetryCount,
		&task.MaxRetry,
		&task.LastError,
		&task.ScheduledAt,
		&task.StartedAt,
		&task.FinishedAt,
		&task.IdempotencyKey,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.Task{}, domainrepo.ErrNotFound
	}
	return task, err
}

func (s *Store) ListRecentEvents(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error) {
	return s.queryRows(ctx, `
SELECT id, tenant_id, source, event_type, event_id, business_key, trace_id, status, dedup_key, dedup_bucket, occurred_at, created_at
FROM events
WHERE tenant_id = ?
ORDER BY id DESC
LIMIT ?`, tenantID, limit)
}

func (s *Store) ListRecentWorkflowRuns(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error) {
	return s.queryRows(ctx, `
SELECT id, tenant_id, event_id, workflow_id, trigger_rule_id, status, started_at, finished_at, created_at, updated_at
FROM workflow_runs
WHERE tenant_id = ?
ORDER BY id DESC
LIMIT ?`, tenantID, limit)
}

func (s *Store) ListRecentWorkflowTasks(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error) {
	return s.queryRows(ctx, `
SELECT id, run_id, node_key, task_type, status, retry_count, max_retry, last_error, scheduled_at, started_at, finished_at, created_at, updated_at
FROM workflow_tasks
WHERE run_id IN (SELECT id FROM workflow_runs WHERE tenant_id = ?)
ORDER BY id DESC
LIMIT ?`, tenantID, limit)
}

func (s *Store) ListRecentOutboxEvents(ctx context.Context, tenantID uint64, limit int) ([]map[string]any, error) {
	return s.queryRows(ctx, `
SELECT id, tenant_id, aggregate_type, aggregate_id, event_type, status, retry_count, next_retry_at, published_at, created_at, updated_at
FROM outbox_events
WHERE tenant_id = ?
ORDER BY id DESC
LIMIT ?`, tenantID, limit)
}

func (s *Store) queryRows(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			switch typed := values[i].(type) {
			case []byte:
				row[col] = string(typed)
			default:
				row[col] = typed
			}
		}
		results = append(results, row)
	}

	return results, rows.Err()
}
