package mysqlrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/infra"
	"event-workflow-platform/internal/service/auth"
)

type ReplayResult = workflow.ReplayResult

// ReplayRun re-executes the entire immutable DAG. It never resets an existing task.
func (s *Store) ReplayRun(ctx context.Context, sourceID uint64, requestKey, reason string) (ReplayResult, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.Allows("runs:replay") {
		return ReplayResult{}, auth.ErrUnauthorized
	}
	if sourceID == 0 || len(requestKey) == 0 || len(requestKey) > 128 || strings.TrimSpace(reason) == "" || len(reason) > 255 {
		return ReplayResult{}, domainrepo.ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReplayResult{}, err
	}
	defer tx.Rollback()
	// Same tenant lock used by ingestion: quota checks and generation allocation serialize.
	var maxPending int
	if err := tx.QueryRowContext(ctx, `SELECT max_pending_tasks FROM tenants WHERE id=? AND status='active' FOR UPDATE`, p.TenantID).Scan(&maxPending); err != nil {
		return ReplayResult{}, err
	}
	var existing ReplayResult
	var oldSource uint64
	var oldReason string
	err = tx.QueryRowContext(ctx, `SELECT q.new_run_id,r.replay_generation,q.source_run_id,q.reason FROM run_replay_requests q JOIN workflow_runs r ON r.id=q.new_run_id WHERE q.tenant_id=? AND q.request_key=?`, p.TenantID, requestKey).Scan(&existing.RunID, &existing.Generation, &oldSource, &oldReason)
	if err == nil {
		if oldSource != sourceID || oldReason != reason {
			return ReplayResult{}, workflow.ErrReplayConflict
		}
		existing.Reused = true
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ReplayResult{}, err
	}
	var eventID, workflowID, ruleID, rootID uint64
	var version int
	var raw []byte
	var digest, origin, status string
	var finished sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT event_id,workflow_id,trigger_rule_id,workflow_version,definition_json,definition_digest,snapshot_origin,status,finished_at,COALESCE(replay_of,id) FROM workflow_runs WHERE id=? AND tenant_id=? FOR UPDATE`, sourceID, p.TenantID).Scan(&eventID, &workflowID, &ruleID, &version, &raw, &digest, &origin, &status, &finished, &rootID)
	if errors.Is(err, sql.ErrNoRows) {
		return ReplayResult{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return ReplayResult{}, err
	}
	if origin != "runtime" || !finished.Valid || (status != "failed" && status != "success") {
		return ReplayResult{}, workflow.ErrReplayConflict
	}
	var definition workflow.Definition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&definition); err != nil {
		return ReplayResult{}, err
	}
	_, actualDigest, err := definition.Snapshot()
	if err != nil || actualDigest != digest {
		return ReplayResult{}, workflow.ErrReplayConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_tasks t JOIN workflow_runs r ON r.id=t.run_id WHERE r.tenant_id=? AND t.status IN ('pending','blocked','running','retrying')`, p.TenantID).Scan(&pending); err != nil {
		return ReplayResult{}, err
	}
	if pending+len(definition.Nodes) > maxPending {
		return ReplayResult{}, auth.ErrQuota
	}
	var generation int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(replay_generation),0)+1 FROM workflow_runs WHERE tenant_id=? AND (id=? OR replay_of=?)`, p.TenantID, rootID, rootID).Scan(&generation); err != nil {
		return ReplayResult{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO workflow_runs(tenant_id,event_id,workflow_id,trigger_rule_id,status,workflow_version,definition_json,definition_digest,snapshot_origin,replay_of,replay_generation,replay_reason) VALUES(?,?,?,?,'pending',?,?,?,'runtime',?,?,?)`, p.TenantID, eventID, workflowID, ruleID, version, raw, digest, rootID, generation, reason)
	if err != nil {
		return ReplayResult{}, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return ReplayResult{}, err
	}
	var eventPayload []byte
	var traceID, eventType, sourceEventID string
	if err := tx.QueryRowContext(ctx, `SELECT payload_json,COALESCE(trace_id,''),event_type,event_id FROM events WHERE id=? AND tenant_id=?`, eventID, p.TenantID).Scan(&eventPayload, &traceID, &eventType, &sourceEventID); err != nil {
		return ReplayResult{}, err
	}
	taskIDs := map[string]int64{}
	for _, node := range definition.Nodes {
		initial := "pending"
		if len(node.DependsOn) > 0 {
			initial = "blocked"
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("replay|%d|%s", newID, node.Key)))
		taskResult, err := tx.ExecContext(ctx, `INSERT INTO workflow_tasks(run_id,node_key,task_type,status,retry_count,max_retry,scheduled_at,idempotency_key) VALUES(?,?,?,?,0,3,?,?)`, newID, node.Key, node.Type, initial, time.Now().UTC(), fmt.Sprintf("%x", sum[:16]))
		if err != nil {
			return ReplayResult{}, err
		}
		taskID, err := taskResult.LastInsertId()
		if err != nil {
			return ReplayResult{}, err
		}
		taskIDs[node.Key] = taskID
		payload, err := json.Marshal(map[string]any{"tenant_id": p.TenantID, "event_id": eventID, "run_id": newID, "task_id": taskID, "task_type": node.Type, "config": node.Config, "trace_id": traceID, "payload": json.RawMessage(eventPayload), "source_event_id": sourceEventID, "event_type": eventType, "trace_context": infra.TraceCarrier(ctx)})
		if err != nil {
			return ReplayResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(tenant_id,aggregate_type,aggregate_id,event_type,payload_json,status) VALUES(?,'workflow_task',?,'workflow.task.created',?,?)`, p.TenantID, fmt.Sprint(taskID), payload, initial); err != nil {
			return ReplayResult{}, err
		}
	}
	for _, node := range definition.Nodes {
		for _, parent := range node.DependsOn {
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_dependencies(run_id,parent_task_id,child_task_id) VALUES(?,?,?)`, newID, taskIDs[parent], taskIDs[node.Key]); err != nil {
				return ReplayResult{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO run_replay_requests(tenant_id,request_key,source_run_id,new_run_id,reason,subject) VALUES(?,?,?,?,?,?)`, p.TenantID, requestKey, sourceID, newID, reason, p.Subject); err != nil {
		return ReplayResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,key_id,subject,action,resource) VALUES(?,?,?,'run:replay',?)`, p.TenantID, p.KeyID, p.Subject, fmt.Sprint(newID)); err != nil {
		return ReplayResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{RunID: uint64(newID), Generation: generation}, nil
}
