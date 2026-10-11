package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrations.Verify(ctx, db); err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	state := map[string]any{
		"events":         queryRows(db, `SELECT id, tenant_id, source, event_type, event_id, business_key, trace_id, status, dedup_key, dedup_bucket, occurred_at, created_at FROM events ORDER BY id DESC LIMIT 10`),
		"workflow_runs":  queryRows(db, `SELECT id, tenant_id, event_id, workflow_id, trigger_rule_id, status, started_at, finished_at, created_at, updated_at FROM workflow_runs ORDER BY id DESC LIMIT 10`),
		"workflow_tasks": queryRows(db, `SELECT id, run_id, node_key, task_type, status, retry_count, max_retry, last_error, scheduled_at, started_at, finished_at, created_at, updated_at FROM workflow_tasks ORDER BY id DESC LIMIT 10`),
		"outbox_events":  queryRows(db, `SELECT id, tenant_id, aggregate_type, aggregate_id, event_type, status, retry_count, next_retry_at, published_at, created_at, updated_at FROM outbox_events ORDER BY id DESC LIMIT 10`),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		log.Fatal(err)
	}
}

func queryRows(db *sql.DB, query string) []map[string]any {
	rows, err := db.Query(query)
	if err != nil {
		return []map[string]any{{"error": err.Error()}}
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return []map[string]any{{"error": err.Error()}}
	}

	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return []map[string]any{{"error": err.Error()}}
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			switch typed := values[i].(type) {
			case []byte:
				row[col] = string(typed)
			default:
				row[col] = typed
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return []map[string]any{{"error": err.Error()}}
	}

	return results
}
