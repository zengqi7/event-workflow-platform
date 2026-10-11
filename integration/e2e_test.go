//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/app/dto"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	redisrepo "event-workflow-platform/internal/repo/redis"
	"event-workflow-platform/internal/service/notifier"
	httptransport "event-workflow-platform/internal/transport/http"
	workertransport "event-workflow-platform/internal/transport/worker"
)

func TestEventToWebhookAndConcurrentDedup(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_MYSQL_DSN")
	redisAddr := os.Getenv("INTEGRATION_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("integration environment is not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	var deliveries atomic.Int32
	var received map[string]any
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("webhook has no idempotency key")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		deliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	eventType := fmt.Sprintf("integration.%d", time.Now().UnixNano())
	definition, _ := json.Marshal(map[string]any{"nodes": []map[string]any{{"key": "notify_webhook", "type": "webhook", "config": map[string]any{"url": sink.URL, "method": "POST"}}}})
	result, err := db.ExecContext(ctx, `INSERT INTO workflows(tenant_id,name,version,status,definition_json) VALUES(1,?,1,'active',?)`, eventType, definition)
	if err != nil {
		t.Fatal(err)
	}
	workflowID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rules(tenant_id,name,event_type,priority,condition_json,workflow_id) VALUES(1,?,?,1,'{"logic":"AND","conditions":[]}',?)`, eventType, eventType, workflowID); err != nil {
		t.Fatal(err)
	}

	store := mysqlrepo.NewStore(db)
	keyID, token, err := store.CreateKey(ctx, 1, "integration", []string{"events:write", "state:read"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer store.RevokeKey(context.Background(), 1, keyID, "integration-cleanup")
	t.Setenv("EVENT_API_TOKEN", token)
	cache := redisrepo.NewCache(client)
	ingestor := command.NewIngestEventCommand(store, cache)
	api := httptest.NewServer(httptransport.NewServer(":0", ingestor, store, store, cache).Handler)
	defer api.Close()
	eventID := fmt.Sprintf("it-%d", time.Now().UnixNano())
	request := dto.IngestEventRequest{TenantCode: "acme", Source: "integration", EventType: eventType, EventID: eventID, BusinessKey: eventID, OccurredAt: time.Now().UTC().Format(time.RFC3339), Payload: map[string]any{"amount": 6999, "order_id": eventID}}
	first := postEvent(t, api.URL, request)
	second := postEvent(t, api.URL, request)
	if first.EventID != second.EventID {
		t.Fatalf("idempotency returned different records: %d != %d", first.EventID, second.EventID)
	}
	conflicting := request
	conflicting.Payload = map[string]any{"amount": 7000, "order_id": eventID}
	if status := postEventStatus(t, api.URL, conflicting); status != http.StatusConflict {
		t.Fatalf("expected idempotency conflict, got status %d", status)
	}
	runner := workertransport.NewRunner(store, notifier.NewDevelopmentWebhookExecutor(3*time.Second, "127.0.0.1"))
	if err := runner.PollOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if deliveries.Load() != 1 {
		t.Fatalf("expected one webhook delivery, got %d", deliveries.Load())
	}
	payload, _ := received["payload"].(map[string]any)
	if payload["order_id"] != eventID {
		t.Fatalf("business payload was not delivered: %+v", received)
	}
	var taskStatus, runStatus, outboxStatus string
	if err := db.QueryRowContext(ctx, `SELECT t.status,r.status,o.status FROM workflow_tasks t JOIN workflow_runs r ON r.id=t.run_id JOIN outbox_events o ON o.aggregate_id=CAST(t.id AS CHAR) WHERE r.event_id=?`, first.EventID).Scan(&taskStatus, &runStatus, &outboxStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "success" || runStatus != "success" || outboxStatus != "published" {
		t.Fatalf("inconsistent states: task=%s run=%s outbox=%s", taskStatus, runStatus, outboxStatus)
	}

	businessKey := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())
	occurred := time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := request
			req.EventID = fmt.Sprintf("%s-%d", businessKey, i)
			req.BusinessKey = businessKey
			req.OccurredAt = occurred
			_, err := postEventResult(api.URL, req)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	var receivedCount, skippedCount int
	if err := db.QueryRowContext(ctx, `SELECT SUM(status='received'),SUM(status='dedup_skipped') FROM events WHERE business_key=?`, businessKey).Scan(&receivedCount, &skippedCount); err != nil {
		t.Fatal(err)
	}
	if receivedCount != 1 || skippedCount != n-1 {
		t.Fatalf("dedup race produced received=%d skipped=%d", receivedCount, skippedCount)
	}
}

func postEventStatus(t *testing.T, base string, request dto.IngestEventRequest) int {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/events", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("EVENT_API_TOKEN"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
func postEvent(t *testing.T, base string, req dto.IngestEventRequest) dto.IngestEventResponse {
	t.Helper()
	result, err := postEventResult(base, req)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func postEventResult(base string, request dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	raw, _ := json.Marshal(request)
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/events", bytes.NewReader(raw))
	if err != nil {
		return dto.IngestEventResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("EVENT_API_TOKEN"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return dto.IngestEventResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		var body any
		json.NewDecoder(resp.Body).Decode(&body)
		return dto.IngestEventResponse{}, fmt.Errorf("status %d: %v", resp.StatusCode, body)
	}
	var result dto.IngestEventResponse
	return result, json.NewDecoder(resp.Body).Decode(&result)
}
