package workflowengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	domainevent "event-workflow-platform/internal/domain/event"
	"event-workflow-platform/internal/domain/rule"
	"event-workflow-platform/internal/domain/workflow"
)

func BuildRunPlan(evt domainevent.Event, matchedRule rule.Rule, definition workflow.Workflow) (workflow.RunPlan, error) {
	raw, digest, err := definition.Definition.Snapshot()
	if err != nil {
		return workflow.RunPlan{}, err
	}
	tasks := make([]workflow.TaskPlan, 0, len(definition.Definition.Nodes))
	for _, node := range definition.Definition.Nodes {
		tasks = append(tasks, workflow.TaskPlan{
			NodeKey:        node.Key,
			TaskType:       node.Type,
			Config:         node.Config,
			ScheduledAt:    time.Now().UTC(),
			IdempotencyKey: taskIdempotencyKey(evt.Fingerprint, matchedRule.ID, node.Key),
			DependsOn:      append([]string(nil), node.DependsOn...),
		})
	}

	return workflow.RunPlan{
		WorkflowID:       definition.ID,
		TriggerRuleID:    matchedRule.ID,
		Tasks:            tasks,
		WorkflowVersion:  definition.Version,
		DefinitionJSON:   raw,
		DefinitionDigest: digest,
	}, nil
}

func taskIdempotencyKey(fingerprint string, ruleID uint64, nodeKey string) string {
	raw := fmt.Sprintf("%s|%d|%s", fingerprint, ruleID, nodeKey)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}
