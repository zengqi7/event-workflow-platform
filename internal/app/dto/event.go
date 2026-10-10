package dto

type IngestEventRequest struct {
	TenantCode  string         `json:"tenant_code"`
	Source      string         `json:"source"`
	EventType   string         `json:"event_type"`
	EventID     string         `json:"event_id"`
	BusinessKey string         `json:"business_key"`
	OccurredAt  string         `json:"occurred_at"`
	Payload     map[string]any `json:"payload"`
}

type BatchIngestEventRequest struct {
	Items []IngestEventRequest `json:"items"`
}

type IngestEventResponse struct {
	EventID       uint64 `json:"event_id"`
	TenantID      uint64 `json:"tenant_id"`
	TraceID       string `json:"trace_id"`
	Fingerprint   string `json:"fingerprint"`
	DedupKey      string `json:"dedup_key"`
	Status        string `json:"status"`
	WorkflowRuns  int    `json:"workflow_runs"`
	WorkflowTasks int    `json:"workflow_tasks"`
}

type BatchIngestEventResponse struct {
	Items     []BatchIngestEventItem `json:"items"`
	Count     int                    `json:"count"`
	Succeeded int                    `json:"succeeded"`
	Failed    int                    `json:"failed"`
}

type ListResponse struct {
	Items []map[string]any `json:"items"`
	Count int              `json:"count"`
}

type BatchIngestEventItem struct {
	Index  int                  `json:"index"`
	OK     bool                 `json:"ok"`
	Result *IngestEventResponse `json:"result,omitempty"`
	Error  string               `json:"error,omitempty"`
}
