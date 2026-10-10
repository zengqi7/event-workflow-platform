package ruleeval

import (
	"fmt"
	"strings"

	domainevent "event-workflow-platform/internal/domain/event"
	"event-workflow-platform/internal/domain/rule"
)

func Match(evt domainevent.Event, group rule.ConditionGroup) bool {
	logic := strings.ToUpper(strings.TrimSpace(group.Logic))
	if logic == "" {
		logic = "AND"
	}

	if len(group.Conditions) == 0 {
		return true
	}

	switch logic {
	case "OR":
		for _, condition := range group.Conditions {
			if matchCondition(evt, condition) {
				return true
			}
		}
		return false
	default:
		for _, condition := range group.Conditions {
			if !matchCondition(evt, condition) {
				return false
			}
		}
		return true
	}
}

func matchCondition(evt domainevent.Event, condition rule.Condition) bool {
	actual, ok := lookupField(evt, condition.Field)
	if !ok {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(condition.Op)) {
	case "=":
		return fmt.Sprint(actual) == fmt.Sprint(condition.Value)
	case "!=":
		return fmt.Sprint(actual) != fmt.Sprint(condition.Value)
	case ">":
		return compareFloat(actual, condition.Value, func(a, b float64) bool { return a > b })
	case ">=":
		return compareFloat(actual, condition.Value, func(a, b float64) bool { return a >= b })
	case "<":
		return compareFloat(actual, condition.Value, func(a, b float64) bool { return a < b })
	case "<=":
		return compareFloat(actual, condition.Value, func(a, b float64) bool { return a <= b })
	default:
		return false
	}
}

func lookupField(evt domainevent.Event, field string) (any, bool) {
	switch field {
	case "event_type":
		return evt.EventType, true
	case "source":
		return evt.Source, true
	case "business_key":
		return evt.BusinessKey, true
	}

	if strings.HasPrefix(field, "payload.") {
		key := strings.TrimPrefix(field, "payload.")
		value, ok := evt.Payload[key]
		return value, ok
	}

	return nil, false
}

func compareFloat(actual, expected any, comparator func(a, b float64) bool) bool {
	a, ok := toFloat(actual)
	if !ok {
		return false
	}
	b, ok := toFloat(expected)
	if !ok {
		return false
	}
	return comparator(a, b)
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}
