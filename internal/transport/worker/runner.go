package workertransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"event-workflow-platform/internal/domain/outbox"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/infra"
	"event-workflow-platform/internal/service/notifier"
)

type Runner struct {
	store    domainrepo.WorkerStore
	notifier *notifier.WebhookExecutor
}

func NewRunner(store domainrepo.WorkerStore, executor *notifier.WebhookExecutor) *Runner {
	return &Runner{store: store, notifier: executor}
}

func (r *Runner) PollOnce(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	var failures []error
	// Claim just before delivery, so queued tasks cannot outlive their leases.
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		items, err := r.store.ClaimPendingOutbox(ctx, 1)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if len(items) == 0 {
			break
		}
		infra.AddOutboxClaims(len(items))
		for _, item := range items {
			if err := r.handleOutbox(ctx, item); err != nil {
				failures = append(failures, fmt.Errorf("outbox %d: %w", item.ID, err))
			}
		}
	}
	return errors.Join(failures...)
}

func (r *Runner) handleOutbox(ctx context.Context, item outbox.Event) error {
	taskID, runID, body, url, method, parseErr := parseOutboxPayload(item.Payload)
	if parseErr != nil {
		id, _ := strconv.ParseUint(item.AggregateID, 10, 64)
		task := workflow.Task{}
		if id != 0 {
			var err error
			task, err = r.store.GetTaskByID(ctx, id)
			if err != nil {
				return err
			}
		}
		return errors.Join(parseErr, r.store.FinishDelivery(ctx, item, task, "failed", parseErr.Error(), time.Time{}))
	}
	task, err := r.store.GetTaskByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, domainrepo.ErrNotFound) {
			return errors.Join(err, r.store.FinishDelivery(ctx, item, workflow.Task{}, "failed", "workflow task not found", time.Time{}))
		}
		return err
	}
	if task.RunID != runID || task.TaskType != "webhook" {
		return r.store.FinishDelivery(ctx, item, task, "failed", "invalid task/run association or task type", time.Time{})
	}
	if task.Status == "success" {
		return r.store.FinishDelivery(ctx, item, task, "success", "", time.Time{})
	}
	// Stable across every retry. Exactly-once effects require receiver cooperation.
	body["idempotency_key"] = task.IdempotencyKey
	deliveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	execErr := r.notifier.Execute(deliveryCtx, method, url, body)
	if execErr == nil {
		infra.IncWebhookSuccess()
		err := r.store.FinishDelivery(ctx, item, task, "success", "", time.Time{})
		if err == nil {
			infra.IncWorkerTaskSuccess()
		}
		return err
	}
	infra.IncWebhookFailure()
	status := "failed"
	next := time.Time{}
	if task.RetryCount+1 < task.MaxRetry {
		status = "retrying"
		next = time.Now().UTC().Add(backoff(task.RetryCount))
	}
	if err := r.store.FinishDelivery(ctx, item, task, status, execErr.Error(), next); err != nil {
		return err
	}
	if status == "retrying" {
		infra.IncWorkerTaskRetry()
	} else {
		infra.IncWorkerTaskFailure()
	}
	return nil
}

func parseOutboxPayload(payload map[string]any) (taskID uint64, runID uint64, body map[string]any, url, method string, err error) {
	taskID, err = asUint64(payload["task_id"])
	if err != nil {
		return
	}
	runID, err = asUint64(payload["run_id"])
	if err != nil {
		return
	}
	config, ok := payload["config"].(map[string]any)
	if !ok {
		err = fmt.Errorf("invalid config")
		return
	}
	url, ok = config["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		err = fmt.Errorf("missing webhook url")
		return
	}
	method, _ = config["method"].(string)
	if method == "" {
		method = "POST"
	}
	if method != "POST" && method != "PUT" {
		err = fmt.Errorf("unsupported webhook method")
		return
	}
	body = map[string]any{
		"tenant_id": payload["tenant_id"], "event_id": payload["event_id"],
		"run_id": payload["run_id"], "task_id": payload["task_id"],
		"task_type": payload["task_type"], "trace_id": payload["trace_id"],
		"event_type": payload["event_type"], "source_event_id": payload["source_event_id"],
		"payload": payload["payload"], "status": "triggered",
	}
	return
}

func asUint64(value any) (uint64, error) {
	switch n := value.(type) {
	case json.Number:
		id, err := strconv.ParseUint(string(n), 10, 64)
		if err == nil && id > 0 {
			return id, nil
		}
	case float64:
		if n > 0 && n <= 9007199254740991 && math.Trunc(n) == n {
			return uint64(n), nil
		}
	case int:
		if n > 0 {
			return uint64(n), nil
		}
	case int64:
		if n > 0 {
			return uint64(n), nil
		}
	case uint64:
		if n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid identifier %v", value)
}

func backoff(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 30 * time.Second
	case 1:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}
