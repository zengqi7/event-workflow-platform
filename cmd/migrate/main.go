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
		if _, err := conn.ExecContext(ctx, `INSERT INTO workflows(tenant_id,name,version,status,definition_json) SELECT id,'high-value-order-timeout',2,'active',? FROM tenants WHERE code='acme' ON DUPLICATE KEY UPDATE id=id`, definition); err != nil {
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
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var builder strings.Builder
	var statements []string
	delimiter := ";"
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if strings.HasPrefix(line, "DELIMITER ") {
			if strings.TrimSpace(builder.String()) != "" {
				return nil, fmt.Errorf("delimiter changed mid-statement")
			}
			delimiter = strings.TrimSpace(strings.TrimPrefix(line, "DELIMITER "))
			if delimiter == "" {
				return nil, fmt.Errorf("empty delimiter")
			}
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
		if strings.HasSuffix(line, delimiter) {
			statement := strings.TrimSpace(builder.String())
			statement = strings.TrimSuffix(statement, delimiter)
			statements = append(statements, strings.TrimSpace(statement))
			builder.Reset()
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(builder.String()) != "" {
		return nil, fmt.Errorf("unterminated migration statement")
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("no migration statements found in %s", path)
	}
	return statements, nil
}
