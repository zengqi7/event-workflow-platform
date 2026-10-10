package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"event-workflow-platform/internal/app/dto"
	domainrepo "event-workflow-platform/internal/domain/repository"
)

func TestEventGetHandler(t *testing.T) {
	server := newServer(":0", stubIngestor{}, stubInspector{
		events: []map[string]any{
			{"id": 1, "event_type": "order.timeout"},
		},
	}, stubReadiness{}, stubCache{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=10&tenant_id=1", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp dto.ListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected count 1, got %d", resp.Count)
	}
}

func TestEventPostHandler(t *testing.T) {
	server := newServer(":0", stubIngestor{
		resp: dto.IngestEventResponse{
			EventID:       1,
			TenantID:      1,
			TraceID:       "trace-1",
			Fingerprint:   "fp-1",
			DedupKey:      "dedup-1",
			Status:        "received",
			WorkflowRuns:  1,
			WorkflowTasks: 1,
		},
	}, stubInspector{}, stubReadiness{}, stubCache{})

	body, _ := json.Marshal(dto.IngestEventRequest{
		TenantCode: "acme",
		Source:     "order-service",
		EventType:  "order.timeout",
		EventID:    "evt-1",
		OccurredAt: "2026-08-14T10:30:00Z",
		Payload:    map[string]any{"amount": 6999},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestInternalErrorsAreNotExposed(t *testing.T) {
	secret := "mysql connection failed for event_app:password@tcp(mysql:3306)"
	server := newServer(":0", stubIngestor{err: errors.New(secret)}, stubInspector{err: errors.New(secret)}, stubReadiness{}, stubCache{})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewBufferString(`{"tenant_code":"acme"}`)),
		httptest.NewRequest(http.MethodGet, "/api/v1/events?tenant_id=1", nil),
	} {
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", recorder.Code)
		}
		if bytes.Contains(recorder.Body.Bytes(), []byte(secret)) {
			t.Fatalf("internal error leaked in response: %s", recorder.Body.String())
		}
	}
}

func TestBatchEventHandler(t *testing.T) {
	server := newServer(":0", stubIngestor{
		resp: dto.IngestEventResponse{
			EventID:       1,
			TenantID:      1,
			TraceID:       "trace-1",
			Fingerprint:   "fp-1",
			DedupKey:      "dedup-1",
			Status:        "received",
			WorkflowRuns:  1,
			WorkflowTasks: 1,
		},
	}, stubInspector{}, stubReadiness{}, stubCache{})

	body, _ := json.Marshal(dto.BatchIngestEventRequest{
		Items: []dto.IngestEventRequest{
			{
				TenantCode: "acme",
				Source:     "order-service",
				EventType:  "order.timeout",
				EventID:    "evt-1",
				OccurredAt: "2026-08-14T10:30:00Z",
				Payload:    map[string]any{"amount": 6999},
			},
			{
				TenantCode: "acme",
				Source:     "order-service",
				EventType:  "order.timeout",
				EventID:    "evt-2",
				OccurredAt: "2026-08-14T10:31:00Z",
				Payload:    map[string]any{"amount": 7999},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	var resp dto.BatchIngestEventResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 2 || resp.Succeeded != 2 || resp.Failed != 0 {
		t.Fatalf("unexpected batch counts: count=%d success=%d failed=%d", resp.Count, resp.Succeeded, resp.Failed)
	}
}

func TestBatchEventHandlerPartialFailure(t *testing.T) {
	server := newServer(":0", &sequenceIngestor{
		responses: []ingestOutcome{
			{resp: dto.IngestEventResponse{EventID: 1, TenantID: 1, Status: "received"}},
			{err: errors.New("tenant \"bad\" not found")},
		},
	}, stubInspector{}, stubReadiness{}, stubCache{})

	body, _ := json.Marshal(dto.BatchIngestEventRequest{
		Items: []dto.IngestEventRequest{
			{
				TenantCode: "acme",
				Source:     "order-service",
				EventType:  "order.timeout",
				EventID:    "evt-1",
				OccurredAt: "2026-08-14T10:30:00Z",
				Payload:    map[string]any{"amount": 6999},
			},
			{
				TenantCode: "bad",
				Source:     "order-service",
				EventType:  "order.timeout",
				EventID:    "evt-2",
				OccurredAt: "2026-08-14T10:31:00Z",
				Payload:    map[string]any{"amount": 7999},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", rec.Code)
	}

	var resp dto.BatchIngestEventResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Succeeded != 1 || resp.Failed != 1 {
		t.Fatalf("expected success=1 failed=1, got success=%d failed=%d", resp.Succeeded, resp.Failed)
	}
	if !resp.Items[0].OK || resp.Items[1].OK {
		t.Fatal("expected first item success and second item failure")
	}
}

func TestWorkflowAndOutboxListHandlers(t *testing.T) {
	server := newServer(":0", stubIngestor{}, stubInspector{
		runs:   []map[string]any{{"id": 10, "status": "success"}},
		tasks:  []map[string]any{{"id": 20, "status": "success"}},
		outbox: []map[string]any{{"id": 30, "status": "published"}},
	}, stubReadiness{}, stubCache{})

	for _, path := range []string{"/api/v1/workflow-runs", "/api/v1/workflow-tasks", "/api/v1/outbox-events"} {
		req := httptest.NewRequest(http.MethodGet, path+"?tenant_id=1", nil)
		rec := httptest.NewRecorder()
		server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("path %s expected 200, got %d", path, rec.Code)
		}
	}
}

func TestReadyzHandler(t *testing.T) {
	server := newServer(":0", stubIngestor{}, stubInspector{}, stubReadiness{}, stubCache{})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestReadyzHandlerDegraded(t *testing.T) {
	server := newServer(":0", stubIngestor{}, stubInspector{}, stubReadiness{err: errors.New("mysql down")}, stubCache{pingErr: errors.New("redis down")})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

type stubIngestor struct {
	resp dto.IngestEventResponse
	err  error
}

func (s stubIngestor) Execute(context.Context, dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	return s.resp, s.err
}

type sequenceIngestor struct {
	responses []ingestOutcome
	index     int
}

type ingestOutcome struct {
	resp dto.IngestEventResponse
	err  error
}

func (s *sequenceIngestor) Execute(context.Context, dto.IngestEventRequest) (dto.IngestEventResponse, error) {
	outcome := s.responses[s.index]
	s.index++
	return outcome.resp, outcome.err
}

type stubInspector struct {
	events []map[string]any
	runs   []map[string]any
	tasks  []map[string]any
	outbox []map[string]any
	err    error
}

func (s stubInspector) ListRecentEvents(context.Context, uint64, int) ([]map[string]any, error) {
	return s.events, s.err
}

func (s stubInspector) ListRecentWorkflowRuns(context.Context, uint64, int) ([]map[string]any, error) {
	return s.runs, s.err
}

func (s stubInspector) ListRecentWorkflowTasks(context.Context, uint64, int) ([]map[string]any, error) {
	return s.tasks, s.err
}

func (s stubInspector) ListRecentOutboxEvents(context.Context, uint64, int) ([]map[string]any, error) {
	return s.outbox, s.err
}

var _ domainrepo.InspectionStore = (*stubInspector)(nil)

type stubReadiness struct {
	err error
}

func (s stubReadiness) Ping(context.Context) error {
	return s.err
}

type stubCache struct {
	pingErr error
}

func (s stubCache) AcquireIdempotency(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (s stubCache) Ping(context.Context) error {
	return s.pingErr
}
