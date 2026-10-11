package httptransport

import (
	"context"
	"errors"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/service/auth"
	"net/http"
	"strconv"
	"strings"
)

type runReplayer interface {
	ReplayRun(context.Context, uint64, string, string) (workflow.ReplayResult, error)
}

func replayRunHandler(inspector domainrepo.InspectionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"message": "method not allowed"})
			return
		}
		suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/workflow-runs/")
		parts := strings.Split(suffix, "/")
		if len(parts) != 2 || parts[1] != "replay" {
			writeJSON(w, 404, map[string]string{"message": "not found"})
			return
		}
		id, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil || id == 0 {
			writeJSON(w, 400, map[string]string{"message": "invalid run ID"})
			return
		}
		replayer, ok := inspector.(runReplayer)
		if !ok {
			writeJSON(w, 503, map[string]string{"message": "replay unavailable"})
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid replay request"})
			return
		}
		result, err := replayer.ReplayRun(r.Context(), id, r.Header.Get("Idempotency-Key"), req.Reason)
		if err != nil {
			status := 500
			switch {
			case errors.Is(err, domainrepo.ErrInvalidRequest):
				status = 400
			case errors.Is(err, domainrepo.ErrNotFound):
				status = 404
			case errors.Is(err, workflow.ErrReplayConflict):
				status = 409
			case errors.Is(err, auth.ErrUnauthorized):
				status = 403
			case errors.Is(err, auth.ErrQuota):
				status = 429
			}
			writeJSON(w, status, map[string]string{"message": "replay rejected"})
			return
		}
		status := 201
		if result.Reused {
			status = 200
		}
		writeJSON(w, status, result)
	}
}
