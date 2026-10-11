//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"event-workflow-platform/migrations"
	"fmt"
	"testing"
	"time"
)

func TestMigrationUpgradeAndRefusals(t *testing.T) {
	db := emptyWorkflowDB(t)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := migrations.Files.ReadFile("000001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements, err := migrations.Statements(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the old checksum-tracked format, before the dirty column existed.
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations(name VARCHAR(255) PRIMARY KEY,checksum CHAR(64) NOT NULL,applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(name,checksum) VALUES(?,?)`, "000001_init.up.sql", fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Verify(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, update := range []string{
		`UPDATE schema_migrations SET dirty=TRUE WHERE name='000001_init.up.sql'`,
		`UPDATE schema_migrations SET dirty=FALSE,checksum=REPEAT('0',64) WHERE name='000001_init.up.sql'`,
	} {
		if _, err := db.ExecContext(ctx, update); err != nil {
			t.Fatal(err)
		}
		if err := migrations.Verify(ctx, db); err == nil {
			t.Fatal("unsafe startup accepted")
		}
		if err := migrations.Apply(ctx, db); err == nil {
			t.Fatal("unsafe migration accepted")
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE name='000001_init.up.sql'`, fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(name,checksum,dirty) VALUES('999999_future.up.sql',REPEAT('0',64),FALSE)`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Verify(ctx, db); err == nil {
		t.Fatal("downgrade accepted")
	}
	if err := migrations.Apply(ctx, db); err == nil {
		t.Fatal("future schema accepted")
	}
}

func TestUntrackedSchemaIsNotSilentlyAdopted(t *testing.T) {
	db := emptyWorkflowDB(t)
	raw, _ := migrations.Files.ReadFile("000001_init.up.sql")
	statements, err := migrations.Statements(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Apply(context.Background(), db); err == nil {
		t.Fatal("unverified schema adopted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected baseline %d: %v", count, err)
	}
}
