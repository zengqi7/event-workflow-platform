package mysqlrepo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"event-workflow-platform/internal/domain/outbox"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/infra"
)

func (s *Store) claimDAGOutbox(ctx context.Context, limit int) ([]outbox.Event, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid claim limit")
	}
	claimed := []outbox.Event{}
	for range limit {
		item, ok, err := s.claimFairOutbox(ctx)
		if err != nil {
			return nil, err
		}
		if ok {
			claimed = append(claimed, item)
		}
		if !ok {
			break
		}
	}
	return claimed, nil
}

func (s *Store) claimFairOutbox(ctx context.Context) (outbox.Event, bool, error) {
	var item outbox.Event
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, false, err
	}
	defer tx.Rollback()
	var cursor uint64
	if err := tx.QueryRowContext(ctx, `SELECT last_tenant_id FROM scheduler_state WHERE id=1 FOR UPDATE`).Scan(&cursor); err != nil {
		return item, false, err
	}
	// One opportunity per tenant, persisted across workers and process restarts.
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id,MIN(id) FROM outbox_events WHERE (status='pending' AND (next_retry_at IS NULL OR next_retry_at<=UTC_TIMESTAMP(3))) OR (status='processing' AND lease_until<=UTC_TIMESTAMP(3)) GROUP BY tenant_id ORDER BY (tenant_id>?) DESC,tenant_id LIMIT 100`, cursor)
	if err != nil {
		return item, false, err
	}
	type candidate struct{ tenant, id uint64 }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.tenant, &c.id); err != nil {
			rows.Close()
			return item, false, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return item, false, err
	}
	for _, c := range candidates {
		var ok bool
		item, ok, err = s.claimCandidate(ctx, tx, c.id, c.tenant)
		if err != nil {
			return item, false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE scheduler_state SET last_tenant_id=? WHERE id=1`, c.tenant); err != nil {
			return item, false, err
		}
		if ok {
			return item, true, tx.Commit()
		}
	}
	return item, false, tx.Commit()
}

func (s *Store) claimCandidate(ctx context.Context, tx *sql.Tx, id, tenantID uint64) (outbox.Event, bool, error) {
	var item outbox.Event
	var concurrentLimit, rateLimit, count int
	var window sql.NullString
	var minute string
	err := tx.QueryRowContext(ctx, `SELECT max_concurrent_tasks,dispatches_per_minute,dispatch_count,dispatch_window,DATE_FORMAT(UTC_TIMESTAMP(),'%Y%m%d%H%i') FROM tenants WHERE id=? AND status='active' FOR UPDATE SKIP LOCKED`, tenantID).Scan(&concurrentLimit, &rateLimit, &count, &window, &minute)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	if window.String != minute {
		count = 0
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND status='processing' AND lease_until>UTC_TIMESTAMP(3)`, tenantID).Scan(&active); err != nil {
		return item, false, err
	}
	if active >= concurrentLimit || count >= rateLimit {
		return item, false, nil
	}
	var runID uint64
	if err := tx.QueryRowContext(ctx, `SELECT t.run_id FROM outbox_events o JOIN workflow_tasks t ON o.aggregate_type='workflow_task' AND o.aggregate_id=CAST(t.id AS CHAR) WHERE o.id=? AND o.tenant_id=?`, id, tenantID).Scan(&runID); err != nil {
		return item, false, err
	}
	var locked uint64
	err = tx.QueryRowContext(ctx, `SELECT id FROM workflow_runs WHERE id=? FOR UPDATE SKIP LOCKED`, runID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,tenant_id,aggregate_type,aggregate_id,event_type,payload_json,status,retry_count,next_retry_at,published_at,created_at FROM outbox_events WHERE id=? AND ((status='pending' AND (next_retry_at IS NULL OR next_retry_at<=UTC_TIMESTAMP(3))) OR (status='processing' AND lease_until<=UTC_TIMESTAMP(3))) FOR UPDATE`, id).Scan(&item.ID, &item.TenantID, &item.AggregateType, &item.AggregateID, &item.EventType, &raw, &item.Status, &item.RetryCount, &item.NextRetryAt, &item.PublishedAt, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	var task workflow.Task
	err = tx.QueryRowContext(ctx, `SELECT id,node_key,status,max_retry,attempt_count FROM workflow_tasks WHERE id=CAST(? AS UNSIGNED) AND run_id=? FOR UPDATE`, item.AggregateID, runID).Scan(&task.ID, &task.NodeKey, &task.Status, &task.MaxRetry, &task.AttemptCount)
	if err != nil {
		return item, false, err
	}
	task.RunID = runID
	if task.AttemptCount >= task.MaxRetry {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_tasks SET status='failed',last_error='attempt budget exhausted after lease expiry',finished_at=UTC_TIMESTAMP(3) WHERE id=?`, task.ID); err != nil {
			return item, false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status='failed',lease_token=NULL,lease_until=NULL WHERE id=?`, id); err != nil {
			return item, false, err
		}
		if err := settleDependencies(ctx, tx, task, "failed"); err != nil {
			return item, false, err
		}
		return item, false, refreshRun(ctx, tx, runID)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&item.Payload); err != nil {
		return item, false, err
	}
	item.LeaseToken = infra.NewTraceID()
	if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status='processing',lease_token=?,lease_until=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE id=?`, item.LeaseToken, id); err != nil {
		return item, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE workflow_tasks SET status='running',attempt_count=attempt_count+1,started_at=COALESCE(started_at,UTC_TIMESTAMP(3)) WHERE id=CAST(? AS UNSIGNED) AND run_id=? AND status IN ('pending','retrying','running')`, item.AggregateID, runID)
	if err != nil {
		return item, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return item, false, err
	}
	if changed != 1 {
		return item, false, fmt.Errorf("claim of non-runnable task")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=IF(status='pending','running',status),started_at=COALESCE(started_at,UTC_TIMESTAMP(3)) WHERE id=?`, runID); err != nil {
		return item, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tenants SET dispatch_window=?,dispatch_count=? WHERE id=?`, minute, count+1, tenantID); err != nil {
		return item, false, err
	}
	item.Status = outbox.StatusProcessing
	return item, true, nil
}

func settleDependencies(ctx context.Context, tx *sql.Tx, task workflow.Task, status string) error {
	if status == "success" {
		if _, err := tx.ExecContext(ctx, `UPDATE task_dependencies SET status='succeeded' WHERE run_id=? AND parent_task_id=? AND status='pending'`, task.RunID, task.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_tasks t SET t.status='pending' WHERE t.run_id=? AND t.status='blocked' AND NOT EXISTS(SELECT 1 FROM task_dependencies d WHERE d.run_id=t.run_id AND d.child_task_id=t.id AND d.status<>'succeeded')`, task.RunID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE outbox_events o JOIN workflow_tasks t ON o.aggregate_type='workflow_task' AND o.aggregate_id=CAST(t.id AS CHAR) SET o.status='pending' WHERE t.run_id=? AND t.status='pending' AND o.status='blocked'`, task.RunID)
		return err
	}
	if status != "failed" {
		return nil
	}
	// Repeatedly propagate failure only through blocked descendants; independent roots continue.
	for {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_tasks t JOIN task_dependencies d ON d.child_task_id=t.id AND d.run_id=t.run_id JOIN workflow_tasks parent ON parent.id=d.parent_task_id SET t.status='skipped',t.last_error='dependency failed',t.finished_at=UTC_TIMESTAMP(3) WHERE t.run_id=? AND t.status='blocked' AND parent.status IN ('failed','skipped')`, task.RunID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE outbox_events o JOIN workflow_tasks t ON o.aggregate_type='workflow_task' AND o.aggregate_id=CAST(t.id AS CHAR) SET o.status='skipped' WHERE t.run_id=? AND t.status='skipped' AND o.status='blocked'`, task.RunID)
	return err
}

func refreshRun(ctx context.Context, tx *sql.Tx, runID uint64) error {
	_, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=CASE WHEN EXISTS(SELECT 1 FROM workflow_tasks WHERE run_id=? AND status='failed') THEN 'failed' WHEN EXISTS(SELECT 1 FROM workflow_tasks WHERE run_id=? AND status IN ('blocked','pending','running','retrying')) THEN 'running' ELSE 'success' END,finished_at=IF(EXISTS(SELECT 1 FROM workflow_tasks WHERE run_id=? AND status IN ('blocked','pending','running','retrying')),NULL,UTC_TIMESTAMP(3)) WHERE id=?`, runID, runID, runID, runID)
	return err
}
