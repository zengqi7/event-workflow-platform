package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"event-workflow-platform/internal/infra"
	"event-workflow-platform/migrations"
)

func main() {
	cfg := infra.LoadConfig()
	if cfg.MySQLDSN == "" {
		log.Fatal("MYSQL_DSN is required")
	}

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := waitForDatabase(db, 60*time.Second); err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := migrations.Apply(ctx, db); err != nil {
		log.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	if webhookURL := os.Getenv("SEED_WEBHOOK_URL"); webhookURL != "" {
		// Publish a new demo version instead of mutating an immutable definition.
		if cfg.Environment != "docker" && cfg.Environment != "dev" {
			log.Fatal("demo seed is development-only")
		}
		definition, err := json.Marshal(map[string]any{
			"nodes": []map[string]any{{
				"key": "notify_webhook", "type": "webhook",
				"config": map[string]any{"url": webhookURL, "method": "POST"},
			}},
		})
		if err != nil {
			log.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO workflows(tenant_id,name,version,status,definition_json) SELECT id,'high-value-order-timeout',2,'active',? FROM tenants WHERE code='acme' ON DUPLICATE KEY UPDATE id=workflows.id`, definition); err != nil {
			log.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE rules r JOIN tenants t ON t.id=r.tenant_id JOIN workflows old ON old.id=r.workflow_id JOIN workflows w ON w.tenant_id=r.tenant_id AND w.name='high-value-order-timeout' AND w.version=2 SET r.workflow_id=w.id WHERE t.code='acme' AND r.name='order-timeout-high-value' AND old.name='high-value-order-timeout' AND old.version=1`); err != nil {
			log.Fatal(err)
		}
	}
}

func waitForDatabase(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		lastErr = db.PingContext(ctx)
		cancel()
		if lastErr == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("database not ready after %s: %w", timeout, lastErr)
}

func loadStatements(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return migrations.Statements(string(raw))
}
