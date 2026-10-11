package mysqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/service/auth"
)

// A new definition and optional rule binding are published in one transaction.
func (s *Store) PublishWorkflow(ctx context.Context, item workflow.Workflow, ruleID uint64) (uint64, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || p.TenantID != item.TenantID || !p.Allows("workflows:write") {
		return 0, auth.ErrUnauthorized
	}
	if strings.TrimSpace(item.Name) == "" || len(item.Name) > 128 || item.Version < 1 || item.Version > 2147483647 || (item.Status != "active" && item.Status != "draft") {
		return 0, domainrepo.ErrInvalidRequest
	}
	raw, _, err := item.Definition.Snapshot()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", domainrepo.ErrInvalidRequest, err)
	}
	if ruleID != 0 && item.Status != "active" {
		return 0, domainrepo.ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if ruleID != 0 {
		var id uint64
		err := tx.QueryRowContext(ctx, `SELECT id FROM rules WHERE id=? AND tenant_id=? FOR UPDATE`, ruleID, item.TenantID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, domainrepo.ErrNotFound
		}
		if err != nil {
			return 0, err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO workflows(tenant_id,name,version,status,definition_json) VALUES(?,?,?,?,?)`, item.TenantID, item.Name, item.Version, item.Status, raw)
	if isDuplicateKey(err) {
		return 0, workflow.ErrVersionConflict
	}
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if ruleID != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE rules SET workflow_id=? WHERE id=? AND tenant_id=?`, id, ruleID, item.TenantID); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,key_id,subject,action,resource) VALUES(?,?,?,'workflow:publish',?)`, p.TenantID, p.KeyID, p.Subject, fmt.Sprintf("workflow:%d/rule:%d", id, ruleID)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uint64(id), nil
}
