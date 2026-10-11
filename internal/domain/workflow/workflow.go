package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

var ErrVersionConflict = errors.New("workflow version already exists")
var ErrReplayConflict = errors.New("run cannot be replayed or request key reused")

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
	Key       string         `json:"key"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config"`
	DependsOn []string       `json:"depends_on,omitempty"`
}

type RunPlan struct {
	WorkflowID       uint64
	TriggerRuleID    uint64
	Tasks            []TaskPlan
	WorkflowVersion  int
	DefinitionJSON   json.RawMessage
	DefinitionDigest string
}

type ReplayResult struct {
	RunID      uint64 `json:"run_id"`
	Generation int    `json:"generation"`
	Reused     bool   `json:"reused"`
}

type TaskPlan struct {
	NodeKey        string
	TaskType       string
	Config         map[string]any
	ScheduledAt    time.Time
	IdempotencyKey string
	DependsOn      []string
}

var nodeKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func (d Definition) Validate() error {
	if len(d.Nodes) == 0 || len(d.Nodes) > 100 {
		return fmt.Errorf("definition must contain 1 to 100 nodes")
	}
	nodes := map[string]Node{}
	for _, n := range d.Nodes {
		if !nodeKey.MatchString(n.Key) {
			return fmt.Errorf("invalid node key %q", n.Key)
		}
		if _, ok := nodes[n.Key]; ok {
			return fmt.Errorf("duplicate node %q", n.Key)
		}
		if n.Type != "webhook" {
			return fmt.Errorf("unsupported node type %q", n.Type)
		}
		raw, ok := n.Config["url"].(string)
		if !ok || len(raw) > 2048 {
			return fmt.Errorf("invalid webhook URL")
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
			return fmt.Errorf("invalid webhook URL")
		}
		if method, ok := n.Config["method"]; ok && method != "POST" && method != "PUT" {
			return fmt.Errorf("invalid webhook method")
		}
		if _, ok := n.Config["secret"]; ok {
			return fmt.Errorf("inline secrets forbidden; use secret_ref")
		}
		if ref, ok := n.Config["secret_ref"]; ok {
			value, valid := ref.(string)
			if !valid || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(value) {
				return fmt.Errorf("invalid secret_ref")
			}
		}
		nodes[n.Key] = n
	}
	indegree := map[string]int{}
	children := map[string][]string{}
	for _, n := range d.Nodes {
		seen := map[string]bool{}
		for _, parent := range n.DependsOn {
			if _, ok := nodes[parent]; !ok || parent == n.Key || seen[parent] {
				return fmt.Errorf("invalid dependency %q -> %q", parent, n.Key)
			}
			seen[parent] = true
			indegree[n.Key]++
			children[parent] = append(children[parent], n.Key)
		}
	}
	queue := []string{}
	for _, n := range d.Nodes {
		if indegree[n.Key] == 0 {
			queue = append(queue, n.Key)
		}
	}
	visited := 0
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		visited++
		for _, child := range children[key] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if visited != len(d.Nodes) {
		return fmt.Errorf("workflow contains a cycle")
	}
	return nil
}
func (d Definition) Snapshot() (json.RawMessage, string, error) {
	if err := d.Validate(); err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, fmt.Sprintf("%x", sum), nil
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
