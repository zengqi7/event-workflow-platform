package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/app/dto"
	domainrepo "event-workflow-platform/internal/domain/repository"
	"event-workflow-platform/internal/infra"
)

type ingestExecutor interface {
	Execute(ctx context.Context, req dto.IngestEventRequest) (dto.IngestEventResponse, error)
}

func NewServer(addr string, ingestor *command.IngestEventCommand, inspector domainrepo.InspectionStore, readinessStore domainrepo.ReadinessStore, readinessCache domainrepo.Cache) *http.Server {
	return newServer(addr, ingestor, inspector, readinessStore, readinessCache)
}

func newServer(addr string, ingestor ingestExecutor, inspector domainrepo.InspectionStore, readinessStore domainrepo.ReadinessStore, readinessCache domainrepo.Cache) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(readinessStore, readinessCache))
	mux.HandleFunc("/api/v1/events", eventHandler(ingestor, inspector))
	mux.HandleFunc("/api/v1/events/batch", batchEventHandler(ingestor))
	mux.HandleFunc("/api/v1/workflow-runs", listWorkflowRunsHandler(inspector))
	mux.HandleFunc("/api/v1/workflow-tasks", listWorkflowTasksHandler(inspector))
	mux.HandleFunc("/api/v1/outbox-events", listOutboxEventsHandler(inspector))
	mux.HandleFunc("/metrics", metricsHandler)

	return &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func readyzHandler(readinessStore domainrepo.ReadinessStore, readinessCache domainrepo.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		result := map[string]string{"status": "ok"}
		if readinessStore != nil {
			if err := readinessStore.Ping(ctx); err != nil {
				result["mysql"] = err.Error()
				result["status"] = "degraded"
			} else {
				result["mysql"] = "ok"
			}
		}
		if readinessCache != nil {
			if err := readinessCache.Ping(ctx); err != nil {
				result["redis"] = err.Error()
				result["status"] = "degraded"
			} else {
				result["redis"] = "ok"
			}
		}

		if result["status"] != "ok" {
			writeJSON(w, http.StatusServiceUnavailable, result)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func eventHandler(ingestor ingestExecutor, inspector domainrepo.InspectionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			tenantID, err := requestTenant(r)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
				return
			}
			items, err := inspector.ListRecentEvents(r.Context(), tenantID, requestLimit(r, 20))
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, dto.ListResponse{Items: items, Count: len(items)})
		case http.MethodPost:
			var req dto.IngestEventRequest
			if err := decodeBody(w, r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"message": "invalid json body",
				})
				return
			}

			result, err := ingestor.Execute(r.Context(), req)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, domainrepo.ErrInvalidRequest) {
					status = http.StatusBadRequest
				}
				if errors.Is(err, domainrepo.ErrNotFound) {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]string{
					"message": err.Error(),
				})
				return
			}

			writeJSON(w, http.StatusAccepted, result)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"message": "method not allowed",
			})
		}
	}
}

func batchEventHandler(ingestor ingestExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"message": "method not allowed",
			})
			return
		}

		var req dto.BatchIngestEventRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "invalid json body",
			})
			return
		}
		if len(req.Items) == 0 || len(req.Items) > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "items must contain 1 to 100 events",
			})
			return
		}

		infra.AddBatchEvents(len(req.Items))
		results := make([]dto.BatchIngestEventItem, 0, len(req.Items))
		succeeded := 0
		failed := 0
		for index, item := range req.Items {
			result, err := ingestor.Execute(r.Context(), item)
			if err != nil {
				failed++
				results = append(results, dto.BatchIngestEventItem{
					Index: index,
					OK:    false,
					Error: err.Error(),
				})
				continue
			}
			succeeded++
			resultCopy := result
			results = append(results, dto.BatchIngestEventItem{
				Index:  index,
				OK:     true,
				Result: &resultCopy,
			})
		}

		status := http.StatusAccepted
		if failed > 0 {
			status = http.StatusMultiStatus
		}
		writeJSON(w, status, dto.BatchIngestEventResponse{
			Items:     results,
			Count:     len(results),
			Succeeded: succeeded,
			Failed:    failed,
		})
	}
}

func listWorkflowRunsHandler(inspector domainrepo.InspectionStore) http.HandlerFunc {
	return listHandler(func(r *http.Request) ([]map[string]any, error) {
		tenantID, _ := requestTenant(r)
		return inspector.ListRecentWorkflowRuns(r.Context(), tenantID, requestLimit(r, 20))
	})
}

func listWorkflowTasksHandler(inspector domainrepo.InspectionStore) http.HandlerFunc {
	return listHandler(func(r *http.Request) ([]map[string]any, error) {
		tenantID, _ := requestTenant(r)
		return inspector.ListRecentWorkflowTasks(r.Context(), tenantID, requestLimit(r, 20))
	})
}

func listOutboxEventsHandler(inspector domainrepo.InspectionStore) http.HandlerFunc {
	return listHandler(func(r *http.Request) ([]map[string]any, error) {
		tenantID, _ := requestTenant(r)
		return inspector.ListRecentOutboxEvents(r.Context(), tenantID, requestLimit(r, 20))
	})
}

func listHandler(fetch func(r *http.Request) ([]map[string]any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"message": "method not allowed",
			})
			return
		}

		if _, err := requestTenant(r); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		items, err := fetch(r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, dto.ListResponse{Items: items, Count: len(items)})
	}
}

func requestLimit(r *http.Request, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 || parsed > 100 {
		return fallback
	}
	return parsed
}

func requestTenant(r *http.Request) (uint64, error) {
	id, err := strconv.ParseUint(r.URL.Query().Get("tenant_id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("positive tenant_id is required")
	}
	return id, nil
}

func metricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(infra.RenderMetrics()))
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("method=%s path=%s duration_ms=%d", r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		log.Printf("encode response failed: %v", err)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("body must contain one JSON value")
	}
	return nil
}
