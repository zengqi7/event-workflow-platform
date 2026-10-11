package workertransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"event-workflow-platform/internal/domain/outbox"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/service/notifier"
)

func TestRunnerPollOnceSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := &stubWorkerStore{
		outboxEvents: []outbox.Event{
			{
				ID:       1,
				TenantID: 1,
				Payload: map[string]any{
					"tenant_id": 1.0,
					"event_id":  100.0,
					"run_id":    200.0,
					"task_id":   300.0,
					"task_type": "webhook",
					"trace_id":  "trace-1",
					"config": map[string]any{
						"url":    server.URL,
						"method": "POST",
					},
				},
			},
		},
		task: workflow.Task{
			ID:         300,
			RunID:      200,
			TaskType:   "webhook",
			RetryCount: 0,
			MaxRetry:   3,
		},
	}

	runner := NewRunner(store, notifier.NewDevelopmentWebhookExecutor(2*time.Second, "127.0.0.1"))
	if err := runner.PollOnce(context.Background(), 10); err != nil {
		t.Fatalf("PollOnce returned error: %v", err)
	}

	if !store.taskSuccess {
		t.Fatal("expected task to be marked success")
	}
	if !store.outboxPublished {
		t.Fatal("expected outbox to be marked published")
	}
}

func TestRunnerPollOnceRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	store := &stubWorkerStore{
		outboxEvents: []outbox.Event{
			{
				ID:       1,
				TenantID: 1,
				Payload: map[string]any{
					"tenant_id": 1.0,
					"event_id":  100.0,
					"run_id":    200.0,
					"task_id":   300.0,
					"task_type": "webhook",
					"trace_id":  "trace-1",
					"config": map[string]any{
						"url":    server.URL,
						"method": "POST",
					},
				},
			},
		},
		task: workflow.Task{
			ID:         300,
			RunID:      200,
			TaskType:   "webhook",
			RetryCount: 0,
			MaxRetry:   3,
		},
	}

	runner := NewRunner(store, notifier.NewDevelopmentWebhookExecutor(2*time.Second, "127.0.0.1"))
	if err := runner.PollOnce(context.Background(), 10); err != nil {
		t.Fatalf("PollOnce returned error: %v", err)
	}

	if !store.taskRetried {
		t.Fatal("expected task to be retried")
	}
	if !store.outboxRescheduled {
		t.Fatal("expected outbox to be rescheduled")
	}
	if store.taskFailed {
		t.Fatal("expected task not to be terminally failed")
	}
}

type stubWorkerStore struct {
	outboxEvents      []outbox.Event
	task              workflow.Task
	taskRunning       bool
	taskSuccess       bool
	taskRetried       bool
	taskFailed        bool
	outboxPublished   bool
	outboxRescheduled bool
	outboxFailed      bool
	refreshRunStatus  int
}

func (s *stubWorkerStore) ClaimPendingOutbox(context.Context, int) ([]outbox.Event, error) {
	items := s.outboxEvents
	s.outboxEvents = nil
	return items, nil
}

func (s *stubWorkerStore) FinishDelivery(_ context.Context, _ outbox.Event, _ workflow.Task, status, _ string, _ time.Time) error {
	s.taskSuccess = status == "success"
	s.taskRetried = status == "retrying"
	s.taskFailed = status == "failed"
	s.outboxPublished = s.taskSuccess
	s.outboxRescheduled = s.taskRetried
	s.outboxFailed = s.taskFailed
	return nil
}

func (s *stubWorkerStore) GetTaskByID(context.Context, uint64) (workflow.Task, error) {
	return s.task, nil
}

func (s *stubWorkerStore) MarkTaskRunning(context.Context, uint64) error {
	s.taskRunning = true
	return nil
}

func (s *stubWorkerStore) MarkTaskSuccess(context.Context, uint64) error {
	s.taskSuccess = true
	return nil
}

func (s *stubWorkerStore) MarkTaskForRetry(context.Context, uint64, string, time.Time) error {
	s.taskRetried = true
	return nil
}

func (s *stubWorkerStore) MarkTaskFailed(context.Context, uint64, string) error {
	s.taskFailed = true
	return nil
}

func (s *stubWorkerStore) MarkOutboxPublished(context.Context, uint64) error {
	s.outboxPublished = true
	return nil
}

func (s *stubWorkerStore) RescheduleOutbox(context.Context, uint64, time.Time) error {
	s.outboxRescheduled = true
	return nil
}

func (s *stubWorkerStore) MarkOutboxFailed(context.Context, uint64) error {
	s.outboxFailed = true
	return nil
}

func (s *stubWorkerStore) RefreshRunStatus(context.Context, uint64) error {
	s.refreshRunStatus++
	return nil
}

var _ domainrepo.WorkerStore = (*stubWorkerStore)(nil)
