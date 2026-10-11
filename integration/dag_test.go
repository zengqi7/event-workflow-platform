//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/app/dto"
	"event-workflow-platform/internal/domain/outbox"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/migrations"
	driver "github.com/go-sql-driver/mysql"
)

func isolatedWorkflowDB(t testing.TB) *sql.DB {
	db := emptyWorkflowDB(t)
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func emptyWorkflowDB(t testing.TB) *sql.DB {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MySQL not configured")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ewp_it_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "`"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); admin.Exec("DROP DATABASE `" + name + "`"); admin.Close() })
	return db
}

func createDAGRun(t testing.TB, db *sql.DB, nodes []workflow.Node) (*mysqlrepo.Store, uint64) {
	t.Helper()
	ctx := context.Background()
	raw, _, err := (workflow.Definition{Nodes: nodes}).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO workflows(tenant_id,name,version,status,definition_json) VALUES(1,'dag',1,'active',?)`, raw)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	if _, err := db.Exec(`INSERT INTO rules(tenant_id,name,event_type,condition_json,workflow_id) VALUES(1,'dag','dag.test','{"conditions":[]}',?)`, id); err != nil {
		t.Fatal(err)
	}
	store := mysqlrepo.NewStore(db)
	response, err := command.NewIngestEventCommand(store, nil).Execute(ctx, dto.IngestEventRequest{TenantCode: "acme", Source: "test", EventType: "dag.test", EventID: "dag", OccurredAt: time.Now().UTC().Format(time.RFC3339), Payload: map[string]any{"order_id": "dag"}})
	if err != nil {
		t.Fatal(err)
	}
	var runID uint64
	if err := db.QueryRow(`SELECT id FROM workflow_runs WHERE event_id=?`, response.EventID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	return store, runID
}

func dagNode(key string, parents ...string) workflow.Node {
	return workflow.Node{Key: key, Type: "webhook", Config: map[string]any{"url": "https://example.com/hooks"}, DependsOn: parents}
}
func settleItem(t *testing.T, store *mysqlrepo.Store, item outbox.Event, status string) {
	t.Helper()
	var taskID uint64
	if _, err := fmt.Sscan(item.AggregateID, &taskID); err != nil {
		t.Fatal(err)
	}
	task, err := store.GetTaskByID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishDelivery(context.Background(), item, task, status, "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestDAGConcurrentFanInAndDuplicateSettlement(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, runID := createDAGRun(t, db, []workflow.Node{dagNode("end", "left", "right"), dagNode("left", "root"), dagNode("right", "root"), dagNode("root")})
	ctx := context.Background()
	roots, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(roots) != 1 {
		t.Fatal("only root runnable", len(roots), err)
	}
	settleItem(t, store, roots[0], "success")
	branches, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(branches) != 2 {
		t.Fatal("fanout", len(branches), err)
	}
	var wg sync.WaitGroup
	for _, item := range branches {
		wg.Add(1)
		go func(item outbox.Event) { defer wg.Done(); settleItem(t, store, item, "success") }(item)
	}
	wg.Wait()
	end, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(end) != 1 {
		t.Fatal("fanin must unlock exactly once", len(end), err)
	}
	settleItem(t, store, end[0], "success")
	var taskID uint64
	fmt.Sscan(end[0].AggregateID, &taskID)
	task, err := store.GetTaskByID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishDelivery(ctx, end[0], task, "success", "", time.Time{}); !errors.Is(err, domainrepo.ErrLeaseLost) {
		t.Fatal("duplicate settlement accepted", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM workflow_runs WHERE id=?`, runID).Scan(&status); err != nil || status != "success" {
		t.Fatal(status, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_dependencies WHERE run_id=? AND status='succeeded'`, runID).Scan(&count); err != nil || count != 4 {
		t.Fatal("edge completion", count, err)
	}
}

func TestDAGFailurePropagationAndExpiredLease(t *testing.T) {
	db := isolatedWorkflowDB(t)
	store, runID := createDAGRun(t, db, []workflow.Node{dagNode("root"), dagNode("child", "root"), dagNode("grandchild", "child"), dagNode("independent")})
	ctx := context.Background()
	roots, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(roots) != 2 {
		t.Fatal(len(roots), err)
	}
	for _, item := range roots {
		var taskID uint64
		fmt.Sscan(item.AggregateID, &taskID)
		task, err := store.GetTaskByID(ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.NodeKey == "root" {
			settleItem(t, store, item, "failed")
		} else {
			if _, err := db.Exec(`UPDATE outbox_events SET lease_until=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE id=?`, item.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.FinishDelivery(ctx, item, task, "success", "", time.Time{}); !errors.Is(err, domainrepo.ErrLeaseLost) {
				t.Fatal("expired lease accepted", err)
			}
			reclaimed, err := store.ClaimPendingOutbox(ctx, 1)
			if err != nil || len(reclaimed) != 1 || reclaimed[0].LeaseToken == item.LeaseToken {
				t.Fatal("reclaim failed", err)
			}
			settleItem(t, store, reclaimed[0], "success")
		}
	}
	var skipped int
	var status string
	var finished sql.NullTime
	if err := db.QueryRow(`SELECT COUNT(*) FROM workflow_tasks WHERE run_id=? AND status='skipped'`, runID).Scan(&skipped); err != nil || skipped != 2 {
		t.Fatal("failure did not propagate", skipped, err)
	}
	if err := db.QueryRow(`SELECT status,finished_at FROM workflow_runs WHERE id=?`, runID).Scan(&status, &finished); err != nil || status != "failed" || !finished.Valid {
		t.Fatal("run not settled", status, finished, err)
	}
	pending, err := store.ClaimPendingOutbox(ctx, 100)
	if err != nil || len(pending) != 0 {
		t.Fatal("skipped descendants runnable", err)
	}
}
