package rule

type Rule struct {
	ID          uint64
	TenantID    uint64
	Name        string
	EventType   string
	Priority    int
	Enabled     bool
	StopOnMatch bool
	Condition   ConditionGroup
	WorkflowID  uint64
}

type ConditionGroup struct {
	Logic      string      `json:"logic"`
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}
