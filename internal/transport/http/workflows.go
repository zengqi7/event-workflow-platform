package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/domain/workflow"
	"event-workflow-platform/internal/service/auth"
)

type workflowPublisher interface {
	PublishWorkflow(context.Context, workflow.Workflow, uint64) (uint64, error)
}

func publishWorkflowHandler(inspector domainrepo.InspectionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"message": "method not allowed"})
			return
		}
		p, ok := auth.FromContext(r.Context())
		if !ok {
			writeJSON(w, 401, map[string]string{"message": "credential required"})
			return
		}
		publisher, ok := inspector.(workflowPublisher)
		if !ok {
			writeJSON(w, 503, map[string]string{"message": "publisher unavailable"})
			return
		}
		var req struct {
			Name       string              `json:"name"`
			Version    int                 `json:"version"`
			Status     string              `json:"status"`
			RuleID     uint64              `json:"rule_id"`
			Definition workflow.Definition `json:"definition"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid definition"})
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeJSON(w, 400, map[string]string{"message": "one JSON object required"})
			return
		}
		id, err := publisher.PublishWorkflow(r.Context(), workflow.Workflow{TenantID: p.TenantID, Name: req.Name, Version: req.Version, Status: req.Status, Definition: req.Definition}, req.RuleID)
		if err != nil {
			status := 500
			switch {
			case errors.Is(err, domainrepo.ErrInvalidRequest):
				status = 400
			case errors.Is(err, domainrepo.ErrNotFound):
				status = 404
			case errors.Is(err, auth.ErrUnauthorized):
				status = 403
			case errors.Is(err, workflow.ErrVersionConflict):
				status = 409
			}
			writeJSON(w, status, map[string]string{"message": "workflow publication failed"})
			return
		}
		writeJSON(w, 201, map[string]uint64{"workflow_id": id})
	}
}
