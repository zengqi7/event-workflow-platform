//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/internal/service/auth"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialLifecycleAndDistributedAdmission(t *testing.T) {
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
	code := fmt.Sprintf("auth-%d", time.Now().UnixNano())
	result, err := db.ExecContext(ctx, `INSERT INTO tenants(name,code,requests_per_minute) VALUES('test',?,3)`, code)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	store := mysqlrepo.NewStore(db)
	keyID, token, err := store.CreateKey(ctx, uint64(id), "ci", []string{"state:read"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, token)
	if err != nil || p.TenantID != uint64(id) || p.TenantCode != code {
		t.Fatalf("credential: %+v %v", p, err)
	}
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT token_hash FROM api_keys WHERE id=?`, keyID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token || stored != auth.Hash(token) {
		t.Fatal("raw token persisted")
	}
	var wg sync.WaitGroup
	var admitted atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.Admit(ctx, p, "GET", "/api/v1/events")
			if err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, auth.ErrQuota) {
				t.Errorf("admission: %v", err)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("quota admitted %d", admitted.Load())
	}
	if err := store.RevokeKey(ctx, uint64(id)+1, keyID, "operator"); err == nil {
		t.Fatal("cross-tenant revoke accepted")
	}
	if err := store.RevokeKey(ctx, uint64(id), keyID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(ctx, token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("revoked key accepted", err)
	}
	expiredID, expiredToken, err := store.CreateKey(ctx, uint64(id), "ci", []string{"state:read"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE api_keys SET expires_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 SECOND) WHERE id=?`, expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(ctx, expiredToken); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("expired key accepted", err)
	}
	var audits int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='GET'`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatal("incorrect admission audit", audits)
	}
}
