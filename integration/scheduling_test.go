//go:build integration

package integration

import (
	"context"
	"errors"
	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/app/dto"
	"event-workflow-platform/internal/domain/outbox"
	"event-workflow-platform/internal/domain/workflow"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/internal/service/notifier"
	workertransport "event-workflow-platform/internal/transport/worker"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFairClaimsAndTenantConcurrency(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, _ := createDAGRun(t, db, []workflow.Node{dagNode("a"), dagNode("b"), dagNode("c"), dagNode("d")})
	if _, err := db.Exec(`INSERT INTO tenants(id,name,code,status,max_concurrent_tasks) VALUES(2,'small','small','active',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tenants SET max_concurrent_tasks=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	raw, _, err := (workflow.Definition{Nodes: []workflow.Node{dagNode("small")}}).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO workflows(tenant_id,name,version,status,definition_json) VALUES(2,'small',1,'active',?)`, raw)
	if err != nil {
		t.Fatal(err)
	}
	workflowID, _ := result.LastInsertId()
	if _, err := db.Exec(`INSERT INTO rules(tenant_id,name,event_type,condition_json,workflow_id) VALUES(2,'small','small.test','{"conditions":[]}',?)`, workflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := command.NewIngestEventCommand(store, nil).Execute(context.Background(), dto.IngestEventRequest{TenantCode: "small", Source: "test", EventType: "small.test", EventID: "small", OccurredAt: time.Now().UTC().Format(time.RFC3339), Payload: map[string]any{"order_id": "small"}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	tenantIDs := make(chan uint64, 8)
	for range 8 {
		wg.Go(func() {
			items, err := store.ClaimPendingOutbox(context.Background(), 1)
			if err != nil {
				t.Error(err)
				return
			}
			for _, item := range items {
				tenantIDs <- item.TenantID
			}
		})
	}
	wg.Wait()
	close(tenantIDs)
	counts := map[uint64]int{}
	for tenant := range tenantIDs {
		counts[tenant]++
	}
	if counts[1] != 1 || counts[2] != 1 {
		t.Fatal("fairness/concurrency violated", counts)
	}
	metrics, err := store.QueueMetrics(context.Background())
	if err != nil || !strings.Contains(metrics, `outbox_queue_depth{state="processing"} 2`) {
		t.Fatal("queue gauges", metrics, err)
	}
}

func TestTenantDispatchMinuteQuota(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, _ := createDAGRun(t, db, []workflow.Node{dagNode("a"), dagNode("b")})
	if _, err := db.Exec(`UPDATE tenants SET dispatches_per_minute=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	items, err := store.ClaimPendingOutbox(context.Background(), 1)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	settleItem(t, store, items[0], "success")
	items, err = store.ClaimPendingOutbox(context.Background(), 1)
	if err != nil || len(items) != 0 {
		t.Fatal("rate quota bypassed", items, err)
	}
	if _, err := db.Exec(`UPDATE tenants SET dispatch_window='200001010000' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	items, err = store.ClaimPendingOutbox(context.Background(), 1)
	if err != nil || len(items) != 1 {
		t.Fatal("minute did not reset", items, err)
	}
}

type lostSettlement struct {
	*mysqlrepo.Store
	fail bool
}

func (s *lostSettlement) FinishDelivery(ctx context.Context, item outbox.Event, task workflow.Task, status, lastError string, next time.Time) error {
	if s.fail {
		s.fail = false
		return errors.New("injected database outage after receiver accepted")
	}
	return s.Store.FinishDelivery(ctx, item, task, status, lastError, next)
}
func TestReceiverAcceptedSettlementLostAndRecovered(t *testing.T) {
	seen := map[string]bool{}
	deliveries, effects := 0, 0
	var receiverMu sync.Mutex
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverMu.Lock()
		defer receiverMu.Unlock()
		deliveries++
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			t.Error("no delivery key")
		}
		if !seen[key] {
			effects++
			seen[key] = true
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	node := dagNode("root")
	node.Config["url"] = receiver.URL
	db := isolatedWorkflowDB(t)
	store, runID := createDAGRun(t, db, []workflow.Node{node})
	failing := &lostSettlement{Store: store, fail: true}
	runner := workertransport.NewRunner(failing, notifier.NewDevelopmentWebhookExecutor(time.Second, "127.0.0.1"))
	if err := runner.PollOnce(context.Background(), 1); err == nil {
		t.Fatal("fault injection ineffective")
	}
	if _, err := db.Exec(`UPDATE outbox_events SET lease_until=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE status='processing'`); err != nil {
		t.Fatal(err)
	}
	if err := runner.PollOnce(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var state string
	receiverMu.Lock()
	defer receiverMu.Unlock()
	if err := db.QueryRow(`SELECT status FROM workflow_runs WHERE id=?`, runID).Scan(&state); err != nil || state != "success" || deliveries != 2 || effects != 1 {
		t.Fatal("recovery or receiver idempotency failed", state, deliveries, effects, err)
	}
}

func BenchmarkClaimReadyTask(b *testing.B) {
	db := isolatedWorkflowDB(b)
	store, runID := createDAGRun(b, db, []workflow.Node{dagNode("root")})
	if _, err := db.Exec(`UPDATE tenants SET dispatches_per_minute=2147483647 WHERE id=1`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if _, err := db.Exec(`UPDATE workflow_tasks SET status='pending',attempt_count=0 WHERE run_id=?`, runID); err != nil {
			b.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE outbox_events SET status='pending',lease_token=NULL,lease_until=NULL,next_retry_at=NULL WHERE aggregate_type='workflow_task'`); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		items, err := store.ClaimPendingOutbox(context.Background(), 1)
		if err != nil || len(items) != 1 {
			b.Fatal(items, err)
		}
	}
}

func TestCrashReclaimExhaustsBudgetAndSkipsDescendants(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, runID := createDAGRun(t, db, []workflow.Node{dagNode("root"), dagNode("child", "root")})
	for attempt := 1; attempt <= 3; attempt++ {
		items, err := store.ClaimPendingOutbox(context.Background(), 1)
		if err != nil || len(items) != 1 {
			t.Fatal("crash claim", attempt, items, err)
		}
		var taskID uint64
		if _, err := fmt.Sscan(items[0].AggregateID, &taskID); err != nil {
			t.Fatal(err)
		}
		task, err := store.GetTaskByID(context.Background(), taskID)
		if err != nil || task.AttemptCount != attempt {
			t.Fatal("attempt not charged at claim", task, err)
		}
		if _, err := db.Exec(`UPDATE outbox_events SET lease_until=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE id=?`, items[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ClaimPendingOutbox(context.Background(), 1)
	if err != nil || len(items) != 0 {
		t.Fatal("crash budget unlimited", items, err)
	}
	var failed, skipped int
	if err := db.QueryRow(`SELECT SUM(status='failed'),SUM(status='skipped') FROM workflow_tasks WHERE run_id=?`, runID).Scan(&failed, &skipped); err != nil || failed != 1 || skipped != 1 {
		t.Fatal("terminal propagation", failed, skipped, err)
	}
}

func TestDurableTracePropagationToWebhook(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	prop := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(prop)
		provider.Shutdown(context.Background())
	}()
	headers := make(chan string, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("traceparent")
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	node := dagNode("root")
	node.Config["url"] = receiver.URL
	db := isolatedWorkflowDB(t)
	store, _ := createDAGRun(t, db, []workflow.Node{node})
	runner := workertransport.NewRunner(store, notifier.NewDevelopmentWebhookExecutor(time.Second, "127.0.0.1"))
	if err := runner.PollOnce(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	header := <-headers
	if len(spans) != 3 || header == "" {
		t.Fatal("missing spans/trace header", len(spans), header)
	}
	traceID := spans[0].SpanContext.TraceID()
	for _, span := range spans {
		if span.SpanContext.TraceID() != traceID {
			t.Fatal("trace broken across outbox")
		}
	}
	if !strings.Contains(header, fmt.Sprint(traceID)) {
		t.Fatal("receiver got another trace", header, traceID)
	}
}
