package infra

import (
	"fmt"
	"strings"
	"sync/atomic"
)

var metricsRegistry = struct {
	eventIngestTotal       atomic.Int64
	eventBatchTotal        atomic.Int64
	eventDedupSkippedTotal atomic.Int64
	eventIdempotencyHits   atomic.Int64
	workflowRunTotal       atomic.Int64
	workflowTaskTotal      atomic.Int64
	workerOutboxClaimTotal atomic.Int64
	workerTaskSuccessTotal atomic.Int64
	workerTaskRetryTotal   atomic.Int64
	workerTaskFailureTotal atomic.Int64
	webhookSuccessTotal    atomic.Int64
	webhookFailureTotal    atomic.Int64
}{}

func IncEventIngest() {
	metricsRegistry.eventIngestTotal.Add(1)
}

func AddBatchEvents(v int) {
	metricsRegistry.eventBatchTotal.Add(int64(v))
}

func IncDedupSkipped() {
	metricsRegistry.eventDedupSkippedTotal.Add(1)
}

func IncIdempotencyHit() {
	metricsRegistry.eventIdempotencyHits.Add(1)
}

func AddWorkflowRuns(v int) {
	metricsRegistry.workflowRunTotal.Add(int64(v))
}

func AddWorkflowTasks(v int) {
	metricsRegistry.workflowTaskTotal.Add(int64(v))
}

func AddOutboxClaims(v int) {
	metricsRegistry.workerOutboxClaimTotal.Add(int64(v))
}

func IncWorkerTaskSuccess() {
	metricsRegistry.workerTaskSuccessTotal.Add(1)
}

func IncWorkerTaskRetry() {
	metricsRegistry.workerTaskRetryTotal.Add(1)
}

func IncWorkerTaskFailure() {
	metricsRegistry.workerTaskFailureTotal.Add(1)
}

func IncWebhookSuccess() {
	metricsRegistry.webhookSuccessTotal.Add(1)
}

func IncWebhookFailure() {
	metricsRegistry.webhookFailureTotal.Add(1)
}

func RenderMetrics() string {
	lines := []string{
		formatCounter("event_ingest_total", metricsRegistry.eventIngestTotal.Load()),
		formatCounter("event_batch_total", metricsRegistry.eventBatchTotal.Load()),
		formatCounter("event_dedup_skipped_total", metricsRegistry.eventDedupSkippedTotal.Load()),
		formatCounter("event_idempotency_hits_total", metricsRegistry.eventIdempotencyHits.Load()),
		formatCounter("workflow_run_total", metricsRegistry.workflowRunTotal.Load()),
		formatCounter("workflow_task_total", metricsRegistry.workflowTaskTotal.Load()),
		formatCounter("worker_outbox_claim_total", metricsRegistry.workerOutboxClaimTotal.Load()),
		formatCounter("worker_task_success_total", metricsRegistry.workerTaskSuccessTotal.Load()),
		formatCounter("worker_task_retry_total", metricsRegistry.workerTaskRetryTotal.Load()),
		formatCounter("worker_task_failure_total", metricsRegistry.workerTaskFailureTotal.Load()),
		formatCounter("webhook_success_total", metricsRegistry.webhookSuccessTotal.Load()),
		formatCounter("webhook_failure_total", metricsRegistry.webhookFailureTotal.Load()),
	}

	return strings.Join(lines, "\n") + "\n"
}

func formatCounter(name string, value int64) string {
	return fmt.Sprintf("# TYPE %s counter\n%s %d", name, name, value)
}
