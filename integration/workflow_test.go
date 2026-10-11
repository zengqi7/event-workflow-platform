//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/app/dto"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/internal/service/auth"
	httptransport "event-workflow-platform/internal/transport/http"
)

func TestWorkflowPublicationAndHistoricalSnapshot(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MySQL not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code := fmt.Sprintf("workflow-%d", time.Now().UnixNano())
	result, err := db.ExecContext(ctx, `INSERT INTO tenants(name,code) VALUES('workflow test',?)`, code)
	if err != nil {
		t.Fatal(err)
	}
	tenantID, _ := result.LastInsertId()
	store := mysqlrepo.NewStore(db)
	_, token, err := store.CreateKey(ctx, uint64(tenantID), "publisher", []string{"workflows:write", "events:write"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	authenticated := auth.WithPrincipal(ctx, p)
	def := workflow.Definition{Nodes: []workflow.Node{{Key: "notify", Type: "webhook", Config: map[string]any{"url": "https://example.com/v1"}}}}
	item := workflow.Workflow{TenantID: p.TenantID, Name: "order", Version: 1, Status: "active", Definition: def}
	api := httptest.NewServer(httptransport.NewServer(":0", command.NewIngestEventCommand(store, nil), store, store, nil).Handler)
	defer api.Close()
	raw, _ := json.Marshal(map[string]any{"name": item.Name, "version": item.Version, "status": item.Status, "definition": item.Definition})
	request, _ := http.NewRequest("POST", api.URL+"/api/v1/workflows", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatal("publish HTTP status", response.StatusCode)
	}
	var published struct {
		ID uint64 `json:"workflow_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&published); err != nil || published.ID == 0 {
		t.Fatal("invalid publication", err)
	}
	if _, err := store.PublishWorkflow(authenticated, item, 0); !errors.Is(err, workflow.ErrVersionConflict) {
		t.Fatal("duplicate version allowed", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE workflows SET definition_json='{"nodes":[]}' WHERE id=?`, published.ID); err == nil {
		t.Fatal("immutable definition modified")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workflows WHERE id=?`, published.ID); err == nil {
		t.Fatal("immutable version deleted")
	}
	result, err = db.ExecContext(ctx, `INSERT INTO rules(tenant_id,name,event_type,condition_json,workflow_id) VALUES(?,'order','order.created','{"conditions":[]}',?)`, tenantID, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	ruleID, _ := result.LastInsertId()
	ingestor := command.NewIngestEventCommand(store, nil)
	ingested, err := ingestor.Execute(ctx, dto.IngestEventRequest{TenantCode: code, Source: "test", EventType: "order.created", EventID: "one", OccurredAt: time.Now().UTC().Format(time.RFC3339), Payload: map[string]any{"order_id": "one"}})
	if err != nil {
		t.Fatal(err)
	}
	item.Version = 2
	item.Definition.Nodes[0].Config["url"] = "https://example.com/v2"
	if _, err := store.PublishWorkflow(authenticated, item, uint64(ruleID)); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	var version int
	var digest, origin string
	if err := db.QueryRowContext(ctx, `SELECT workflow_version,definition_json,definition_digest,snapshot_origin FROM workflow_runs WHERE event_id=?`, ingested.EventID).Scan(&version, &snapshot, &digest, &origin); err != nil {
		t.Fatal(err)
	}
	var historical workflow.Definition
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.UseNumber()
	if err := decoder.Decode(&historical); err != nil {
		t.Fatal(err)
	}
	_, expected, err := historical.Snapshot()
	if err != nil || version != 1 || origin != "runtime" || digest != expected || historical.Nodes[0].Config["url"] != "https://example.com/v1" {
		t.Fatalf("historical snapshot changed: %d %s %s %v", version, origin, snapshot, err)
	}
	item.Version = 3
	if _, err := store.PublishWorkflow(authenticated, item, uint64(ruleID)+999999); !errors.Is(err, domainrepo.ErrNotFound) {
		t.Fatal("foreign rule binding allowed", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflows WHERE tenant_id=? AND version=3`, tenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("publication rollback failed", err)
	}
}
