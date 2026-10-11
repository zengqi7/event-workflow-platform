package httptransport

import (
	"context"
	"errors"
	"event-workflow-platform/internal/app/dto"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/service/auth"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type tenantIngestor struct{ next ingestExecutor }

func (t tenantIngestor) Execute(ctx context.Context, req dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return dto.IngestEventResponse{}, auth.ErrUnauthorized
	}
	if req.TenantCode != "" && strings.ToLower(strings.TrimSpace(req.TenantCode)) != p.TenantCode {
		return dto.IngestEventResponse{}, domainrepo.ErrInvalidRequest
	}
	req.TenantCode = p.TenantCode
	return t.next.Execute(ctx, req)
}

func authorization(next http.Handler, repo auth.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if repo == nil {
			writeJSON(w, 503, map[string]string{"message": "authentication unavailable"})
			return
		}
		value := r.Header.Get("Authorization")
		if !strings.HasPrefix(value, "Bearer ") {
			writeJSON(w, 401, map[string]string{"message": "Bearer credential required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		p, err := repo.Authenticate(ctx, strings.TrimPrefix(value, "Bearer "))
		if err != nil {
			status := 503
			if errors.Is(err, auth.ErrUnauthorized) {
				status = 401
			}
			writeJSON(w, status, map[string]string{"message": "credential unavailable or invalid"})
			return
		}
		scope := "state:read"
		if r.Method != http.MethodGet {
			scope = "events:write"
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/workflows") && r.Method != http.MethodGet {
			scope = "workflows:write"
		}
		if strings.HasSuffix(r.URL.Path, "/replay") {
			scope = "runs:replay"
		}
		if !p.Allows(scope) {
			writeJSON(w, 403, map[string]string{"message": "insufficient scope"})
			return
		}
		if raw := r.URL.Query().Get("tenant_id"); raw != "" && raw != strconv.FormatUint(p.TenantID, 10) {
			writeJSON(w, 403, map[string]string{"message": "tenant mismatch"})
			return
		}
		if err := repo.Admit(ctx, p, r.Method, r.URL.Path); err != nil {
			status := 503
			if errors.Is(err, auth.ErrQuota) {
				status = 429
				w.Header().Set("Retry-After", "60")
			}
			writeJSON(w, status, map[string]string{"message": "request admission failed"})
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(ctx, p)))
	})
}
