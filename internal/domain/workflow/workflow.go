package workflow

import "time"

type Workflow struct {
	ID         uint64
	TenantID   uint64
	Name       string
	Version    int
	Status     string
	Definition Definition
}

type Definition struct {
	Nodes []Node `json:"nodes"`
}

type Node struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

type RunPlan struct {
	WorkflowID    uint64
	TriggerRuleID uint64
	Tasks         []TaskPlan
}

type TaskPlan struct {
	NodeKey        string
	TaskType       string
	Config         map[string]any
	ScheduledAt    time.Time
	IdempotencyKey string
}

type Task struct {
	ID             uint64
	RunID          uint64
	NodeKey        string
	TaskType       string
	Status         string
	RetryCount     int
	MaxRetry       int
	LastError      string
	ScheduledAt    time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	IdempotencyKey string
}

const (
	RunStatusPending = "pending"
	RunStatusRunning = "running"
	RunStatusSuccess = "success"
	RunStatusFailed  = "failed"

	TaskStatusPending  = "pending"
	TaskStatusRunning  = "running"
	TaskStatusSuccess  = "success"
	TaskStatusRetrying = "retrying"
	TaskStatusFailed   = "failed"
)
