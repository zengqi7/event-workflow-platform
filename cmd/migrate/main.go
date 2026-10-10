package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"event-workflow-platform/internal/infra"
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
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('ewp_migrations', 10)").Scan(&locked); err != nil || locked != 1 {
		log.Fatal("could not acquire migration lock")
	}
	defer conn.ExecContext(ctx, "SELECT RELEASE_LOCK('ewp_migrations')")
	if _, err := conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name VARCHAR(255) PRIMARY KEY, checksum CHAR(64) NOT NULL, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		log.Fatal(err)
	}
	paths, err := filepath.Glob("migrations/*.up.sql")
	if err != nil || len(paths) == 0 {
		log.Fatal("no migrations found")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(raw))
		name := filepath.Base(path)
		var old string
		err = conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE name = ?", name).Scan(&old)
		if err == nil {
			if old != checksum {
				log.Fatalf("checksum mismatch for %s", name)
			}
			continue
		}
		if err != sql.ErrNoRows {
			log.Fatal(err)
		}
		// Adopt only the complete legacy V1 schema, without rerunning its seed inserts.
		if name == "000001_init.up.sql" {
			var count int
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('tenants','events','rules','workflows','workflow_runs','workflow_tasks','outbox_events')").Scan(&count); err != nil {
				log.Fatal(err)
			}
			if count != 0 && count != 7 {
				log.Fatal("partial legacy schema; inspect before migration")
			}
			if count == 7 {
				if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations(name, checksum) VALUES (?, ?)", name, checksum); err != nil {
					log.Fatal(err)
				}
				continue
			}
		}
		statements, err := loadStatements(path)
		if err != nil {
			log.Fatal(err)
		}
		for i, stmt := range statements {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				log.Fatalf("migration %s statement %d failed: %v; MySQL DDL is not transactional, inspect before retrying", name, i+1, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations(name, checksum) VALUES (?, ?)", name, checksum); err != nil {
			log.Fatal(err)
		}
		log.Printf("applied %s (%d statements)", name, len(statements))
	}
	if webhookURL := os.Getenv("SEED_WEBHOOK_URL"); webhookURL != "" {
		definition, err := json.Marshal(map[string]any{
			"nodes": []map[string]any{{
				"key": "notify_webhook", "type": "webhook",
				"config": map[string]any{"url": webhookURL, "method": "POST"},
			}},
		})
		if err != nil {
			log.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `
UPDATE workflows SET definition_json = ?
WHERE tenant_id = (SELECT id FROM tenants WHERE code = 'acme')
  AND name = 'high-value-order-timeout' AND version = 1`, definition); err != nil {
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
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var builder strings.Builder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	rawParts := strings.Split(builder.String(), ";")
	statements := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		statements = append(statements, stmt)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("no migration statements found in %s", path)
	}
	return statements, nil
}
