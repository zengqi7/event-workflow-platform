package mysqlrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"event-workflow-platform/internal/service/auth"
	"time"
)

func (s *Store) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
	var p auth.Principal
	var scopes []byte
	if len(token) < 32 || len(token) > 128 {
		return p, auth.ErrUnauthorized
	}
	err := s.db.QueryRowContext(ctx, `SELECT k.id,k.tenant_id,t.code,k.subject,k.scopes_json FROM api_keys k JOIN tenants t ON t.id=k.tenant_id WHERE k.token_hash=? AND k.revoked_at IS NULL AND k.expires_at>UTC_TIMESTAMP(3) AND t.status='active'`, auth.Hash(token)).Scan(&p.KeyID, &p.TenantID, &p.TenantCode, &p.Subject, &scopes)
	if errors.Is(err, sql.ErrNoRows) {
		return p, auth.ErrUnauthorized
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(scopes, &p.Scopes)
	return p, err
}

// Serialize admission on the tenant row so quotas hold across API replicas.
func (s *Store) Admit(ctx context.Context, p auth.Principal, action, resource string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var limit int
	if err := tx.QueryRowContext(ctx, `SELECT requests_per_minute FROM tenants WHERE id=? AND status='active' FOR UPDATE`, p.TenantID).Scan(&limit); err != nil {
		return err
	}
	var minute int64
	if err := tx.QueryRowContext(ctx, `SELECT FLOOR(UNIX_TIMESTAMP(UTC_TIMESTAMP())/60)`).Scan(&minute); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_request_windows(tenant_id,window_minute,request_count) VALUES(?,?,0) ON DUPLICATE KEY UPDATE request_count=IF(window_minute=VALUES(window_minute),request_count,0),window_minute=VALUES(window_minute)`, p.TenantID, minute); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT request_count FROM tenant_request_windows WHERE tenant_id=?`, p.TenantID).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return auth.ErrQuota
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tenant_request_windows SET request_count=request_count+1 WHERE tenant_id=?`, p.TenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,key_id,subject,action,resource) VALUES(?,?,?,?,?)`, p.TenantID, p.KeyID, p.Subject, action, resource); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateKey(ctx context.Context, tenantID uint64, subject string, scopes []string, expires time.Time) (uint64, string, error) {
	if err := auth.ValidateKey(subject, scopes, expires); err != nil {
		return 0, "", err
	}
	token, err := auth.NewToken()
	if err != nil {
		return 0, "", err
	}
	raw, _ := json.Marshal(scopes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO api_keys(tenant_id,token_hash,subject,scopes_json,expires_at) VALUES(?,?,?,?,?)`, tenantID, auth.Hash(token), subject, raw, expires.UTC())
	if err != nil {
		return 0, "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,key_id,subject,action,resource) VALUES(?,?,?,'key:create','operator-cli')`, tenantID, id, subject); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return uint64(id), token, nil
}
func (s *Store) RevokeKey(ctx context.Context, tenantID, keyID uint64, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=UTC_TIMESTAMP(3) WHERE tenant_id=? AND id=? AND revoked_at IS NULL`, tenantID, keyID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return auth.ErrUnauthorized
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,key_id,subject,action,resource) VALUES(?,?,?,'key:revoke','operator-cli')`, tenantID, keyID, actor); err != nil {
		return err
	}
	return tx.Commit()
}
