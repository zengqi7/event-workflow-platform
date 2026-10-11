package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func rejectUnknown(ctx context.Context, q queryer, items []string) error {
	known := make(map[string]bool, len(items))
	for _, name := range items {
		known[name] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if !known[name] {
			return fmt.Errorf("database has unknown newer migration %s; do not downgrade binary", name)
		}
	}
	return rows.Err()
}

func names() ([]string, error) {
	items, err := fs.Glob(Files, "*.up.sql")
	sort.Strings(items)
	return items, err
}
func Apply(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK('ewp_migrations',10)`).Scan(&locked); err != nil || locked != 1 {
		return errors.New("cannot acquire migration lock")
	}
	defer conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('ewp_migrations')`)
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name VARCHAR(255) PRIMARY KEY,checksum CHAR(64) NOT NULL,dirty BOOLEAN NOT NULL DEFAULT FALSE,applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	var columns int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='schema_migrations' AND column_name='dirty'`).Scan(&columns); err != nil {
		return err
	}
	if columns == 0 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN dirty BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
			return err
		}
	}
	items, err := names()
	if err != nil {
		return err
	}
	if err := rejectUnknown(ctx, conn, items); err != nil {
		return err
	}
	for _, name := range items {
		raw, err := Files.ReadFile(name)
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(raw))
		var old string
		var dirty bool
		err = conn.QueryRowContext(ctx, `SELECT checksum,dirty FROM schema_migrations WHERE name=?`, name).Scan(&old, &dirty)
		if err == nil {
			if old != checksum || dirty {
				return fmt.Errorf("migration %s checksum mismatch or dirty; inspect/restore before retry", name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if name == "000001_init.up.sql" {
			var count int
			if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('tenants','events','rules','workflows','workflow_runs','workflow_tasks','outbox_events')`).Scan(&count); err != nil {
				return err
			}
			if count != 0 && count != 7 {
				return errors.New("partial legacy schema; inspect before migration")
			}
			if count == 7 {
				return errors.New("untracked legacy schema: compare tables, constraints and seed with 000001 before operator baseline; table count alone is insufficient")
			}
		}
		statements, err := Statements(string(raw))
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(name,checksum,dirty) VALUES(?,?,TRUE)`, name, checksum); err != nil {
			return err
		}
		for i, stmt := range statements {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s statement %d: %w; nontransactional DDL, schema is dirty", name, i+1, err)
			}
		}
		if _, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET dirty=FALSE WHERE name=?`, name); err != nil {
			return err
		}
	}
	return verify(ctx, conn)
}
func Verify(ctx context.Context, db *sql.DB) error {
	return verify(ctx, db)
}
func verify(ctx context.Context, db queryer) error {
	items, err := names()
	if err != nil {
		return err
	}
	for _, name := range items {
		raw, err := Files.ReadFile(name)
		if err != nil {
			return err
		}
		var old string
		var dirty bool
		if err := db.QueryRowContext(ctx, `SELECT checksum,dirty FROM schema_migrations WHERE name=?`, name).Scan(&old, &dirty); err != nil {
			return fmt.Errorf("run cmd/migrate before startup: %w", err)
		}
		if dirty || old != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return fmt.Errorf("migration %s dirty or mismatched", name)
		}
	}
	return rejectUnknown(ctx, db, items)
}
