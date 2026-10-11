//go:build integration

package integration

import (
	"context"
	"errors"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/service/auth"
	httptransport "event-workflow-platform/internal/transport/http"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReplaySnapshotNewKeysAndConcurrentIdempotency(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, runID := createDAGRun(t, db, []workflow.Node{dagNode("root"), dagNode("child", "root")})
	_, token, err := store.CreateKey(context.Background(), 1, "operator", []string{"runs:replay"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithPrincipal(context.Background(), p)
	if _, err := store.ReplayRun(ctx, runID, "request", "recovery"); !errors.Is(err, workflow.ErrReplayConflict) {
		t.Fatal("live run replayed", err)
	}
	roots, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(roots) != 1 {
		t.Fatal(err, len(roots))
	}
	settleItem(t, store, roots[0], "failed")
	var wg sync.WaitGroup
	ids := make(chan uint64, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			result, err := store.ReplayRun(ctx, runID, "request", "recovery")
			if err != nil {
				errs <- err
				return
			}
			ids <- result.RunID
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var newID uint64
	for id := range ids {
		if newID != 0 && newID != id {
			t.Fatal("duplicate replay runs")
		}
		newID = id
	}
	if newID == 0 || newID == runID {
		t.Fatal("missing new run")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM workflow_tasks a JOIN workflow_tasks b ON a.node_key=b.node_key WHERE a.run_id=? AND b.run_id=? AND a.idempotency_key=b.idempotency_key`, runID, newID).Scan(&count); err != nil || count != 0 {
		t.Fatal("replay reused delivery key", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM workflow_tasks WHERE run_id=? AND status='blocked'`, newID).Scan(&count); err != nil || count != 1 {
		t.Fatal("DAG not reset", count, err)
	}
	if _, err := store.ReplayRun(ctx, runID, "request", "different"); !errors.Is(err, workflow.ErrReplayConflict) {
		t.Fatal("request conflict accepted", err)
	}
	other := auth.WithPrincipal(context.Background(), auth.Principal{TenantID: 2, Subject: "other", Scopes: []string{"runs:replay"}})
	// Tenant 2 is created by the test so tenant admission succeeds and isolation is tested.
	if _, err := db.Exec(`INSERT INTO tenants(id,name,code,status) VALUES(2,'other','other','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplayRun(other, runID, "other", "recovery"); !errors.Is(err, domainrepo.ErrNotFound) {
		t.Fatal("cross tenant replay", err)
	}
	items, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(items) != 1 {
		t.Fatal("only replay root runnable", len(items), err)
	}
	settleItem(t, store, items[0], "success")
	items, err = store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(items) != 1 {
		t.Fatal("replay child not unlocked", len(items), err)
	}
	settleItem(t, store, items[0], "success")
	result, err := store.ReplayRun(ctx, newID, "generation2", "repeat")
	if err != nil || result.Generation != 2 {
		t.Fatal("generation allocation", result, err)
	}
	server := httptransport.NewServer(":0", nil, store, nil, nil)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/workflow-runs/%d/replay", runID), strings.NewReader(`{"reason":"API recovery"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "api-replay")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, req)
	if response.Code != 201 {
		t.Fatal("API replay failed", response.Code, response.Body.String())
	}
	if _, err := db.Exec(`UPDATE workflow_runs SET snapshot_origin='legacy_reconstructed' WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplayRun(ctx, runID, "legacy", "repeat"); !errors.Is(err, workflow.ErrReplayConflict) {
		t.Fatal("legacy replay accepted", err)
	}
}
