// internal/scheduler/conditional_engine.go
package scheduler

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-cron-persistent/internal/model"
)

type ConditionalEngine struct {
	scheduler *Scheduler
	evaluator *ConditionEvaluator
}

type ConditionEvaluator struct {
	variables map[string]interface{}
	functions map[string]func(args []interface{}) (interface{}, error)
}

type Condition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
	Logic    string      `json:"logic,omitempty"`
}

type ConditionalTask struct {
	*model.Task
	Conditions    []Condition      `json:"conditions"`
	TruePath      []string         `json:"truePath"`
	FalsePath     []string         `json:"falsePath"`
	ElseCondition *ConditionalTask `json:"elseCondition,omitempty"`
}

type EventTrigger struct {
	Type       string                 `json:"type"`
	Source     string                 `json:"source"`
	Filter     map[string]interface{} `json:"filter"`
	Conditions []Condition            `json:"conditions"`
	Tasks      []string               `json:"tasks"`
	Debounce   time.Duration          `json:"debounce"`
	LastFired  time.Time              `json:"lastFired"`
}

func NewConditionalEngine(scheduler *Scheduler) *ConditionalEngine {
	evaluator := &ConditionEvaluator{
		variables: make(map[string]interface{}),
		functions: make(map[string]func(args []interface{}) (interface{}, error)),
	}

	// Register built-in functions
	evaluator.registerBuiltinFunctions()

	return &ConditionalEngine{
		scheduler: scheduler,
		evaluator: evaluator,
	}
}

func (ce *ConditionalEngine) EvaluateConditions(conditions []Condition, context map[string]interface{}) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	results := make([]bool, len(conditions))

	for i, condition := range conditions {
		result, err := ce.evaluateCondition(condition, context)
		if err != nil {
			return false, fmt.Errorf("error evaluating condition %d: %w", i, err)
		}
		results[i] = result
	}

	// Apply logic operators
	return ce.applyLogic(conditions, results), nil
}

func (ce *ConditionalEngine) evaluateCondition(condition Condition, context map[string]interface{}) (bool, error) {
	fieldValue, exists := context[condition.Field]
	if !exists {
		// Try to get from built-in variables
		fieldValue = ce.getBuiltinValue(condition.Field, context)
	}

	return ce.compareValues(fieldValue, condition.Operator, condition.Value)
}

func (ce *ConditionalEngine) getBuiltinValue(field string, context map[string]interface{}) interface{} {
	now := time.Now()

	switch field {
	case "current_time":
		return now
	case "current_hour":
		return now.Hour()
	case "current_minute":
		return now.Minute()
	case "current_day":
		return int(now.Weekday())
	case "current_date":
		return now.Format("2006-01-02")
	case "task_count":
		return len(ce.scheduler.ListTasks())
	case "system_load":
		// Would integrate with system metrics
		return 0.5
	default:
		// Try nested field access
		if strings.Contains(field, ".") {
			return ce.getNestedValue(field, context)
		}
		return nil
	}
}

func (ce *ConditionalEngine) getNestedValue(field string, context map[string]interface{}) interface{} {
	parts := strings.Split(field, ".")
	current := context

	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}

		if next, exists := current[part].(map[string]interface{}); exists {
			current = next
		} else {
			return nil
		}
	}

	return nil
}

func (ce *ConditionalEngine) compareValues(left interface{}, operator string, right interface{}) (bool, error) {
	switch operator {
	case "eq", "==":
		return ce.isEqual(left, right), nil
	case "ne", "!=":
		return !ce.isEqual(left, right), nil
	case "gt", ">":
		return ce.isGreater(left, right)
	case "gte", ">=":
		return ce.isGreaterOrEqual(left, right)
	case "lt", "<":
		return ce.isLess(left, right)
	case "lte", "<=":
		return ce.isLessOrEqual(left, right)
	case "contains":
		return ce.contains(left, right), nil
	case "matches":
		return ce.matches(left, right)
	case "in":
		return ce.isIn(left, right), nil
	case "exists":
		return left != nil, nil
	default:
		return false, fmt.Errorf("unknown operator: %s", operator)
	}
}

func (ce *ConditionalEngine) isEqual(left, right interface{}) bool {
	return reflect.DeepEqual(left, right)
}

func (ce *ConditionalEngine) isGreater(left, right interface{}) (bool, error) {
	leftVal, err := ce.toFloat64(left)
	if err != nil {
		return false, err
	}
	rightVal, err := ce.toFloat64(right)
	if err != nil {
		return false, err
	}
	return leftVal > rightVal, nil
}

func (ce *ConditionalEngine) isGreaterOrEqual(left, right interface{}) (bool, error) {
	leftVal, err := ce.toFloat64(left)
	if err != nil {
		return false, err
	}
	rightVal, err := ce.toFloat64(right)
	if err != nil {
		return false, err
	}
	return leftVal >= rightVal, nil
}

func (ce *ConditionalEngine) isLess(left, right interface{}) (bool, error) {
	leftVal, err := ce.toFloat64(left)
	if err != nil {
		return false, err
	}
	rightVal, err := ce.toFloat64(right)
	if err != nil {
		return false, err
	}
	return leftVal < rightVal, nil
}

func (ce *ConditionalEngine) isLessOrEqual(left, right interface{}) (bool, error) {
	leftVal, err := ce.toFloat64(left)
	if err != nil {
		return false, err
	}
	rightVal, err := ce.toFloat64(right)
	if err != nil {
		return false, err
	}
	return leftVal <= rightVal, nil
}

func (ce *ConditionalEngine) toFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

func (ce *ConditionalEngine) contains(left, right interface{}) bool {
	leftStr := fmt.Sprintf("%v", left)
	rightStr := fmt.Sprintf("%v", right)
	return strings.Contains(leftStr, rightStr)
}

func (ce *ConditionalEngine) matches(left, right interface{}) (bool, error) {
	// Simple pattern matching - could extend with regex
	leftStr := fmt.Sprintf("%v", left)
	rightStr := fmt.Sprintf("%v", right)

	// Basic wildcard support
	if strings.Contains(rightStr, "*") {
		pattern := strings.ReplaceAll(rightStr, "*", ".*")
		matched, err := regexp.MatchString(pattern, leftStr)
		return matched, err
	}

	return leftStr == rightStr, nil
}

func (ce *ConditionalEngine) isIn(left, right interface{}) bool {
	rightSlice, ok := right.([]interface{})
	if !ok {
		return false
	}

	for _, item := range rightSlice {
		if ce.isEqual(left, item) {
			return true
		}
	}

	return false
}

func (ce *ConditionalEngine) applyLogic(conditions []Condition, results []bool) bool {
	if len(results) == 1 {
		return results[0]
	}

	result := results[0]

	for i := 1; i < len(results); i++ {
		logic := conditions[i].Logic
		if logic == "" {
			logic = "and" // default
		}

		switch strings.ToLower(logic) {
		case "and", "&&":
			result = result && results[i]
		case "or", "||":
			result = result || results[i]
		default:
			result = result && results[i]
		}
	}

	return result
}

func (ce *ConditionEvaluator) registerBuiltinFunctions() {
	ce.functions["now"] = func(args []interface{}) (interface{}, error) {
		return time.Now(), nil
	}

	ce.functions["add"] = func(args []interface{}) (interface{}, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("add function requires 2 arguments")
		}
		left, err := ce.toFloat64(args[0])
		if err != nil {
			return nil, err
		}
		right, err := ce.toFloat64(args[1])
		if err != nil {
			return nil, err
		}
		return left + right, nil
	}

	ce.functions["subtract"] = func(args []interface{}) (interface{}, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("subtract function requires 2 arguments")
		}
		left, err := ce.toFloat64(args[0])
		if err != nil {
			return nil, err
		}
		right, err := ce.toFloat64(args[1])
		if err != nil {
			return nil, err
		}
		return left - right, nil
	}
}

// Event-driven execution
type EventManager struct {
	triggers  map[string]*EventTrigger
	scheduler *Scheduler
	mu        sync.RWMutex
}

func NewEventManager(scheduler *Scheduler) *EventManager {
	return &EventManager{
		triggers:  make(map[string]*EventTrigger),
		scheduler: scheduler,
	}
}

func (em *EventManager) RegisterTrigger(id string, trigger *EventTrigger) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.triggers[id] = trigger
}

func (em *EventManager) ProcessEvent(eventType string, source string, data map[string]interface{}) {
	em.mu.RLock()
	triggers := make([]*EventTrigger, 0)
	for _, trigger := range em.triggers {
		if trigger.Type == eventType && trigger.Source == source {
			triggers = append(triggers, trigger)
		}
	}
	em.mu.RUnlock()

	for _, trigger := range triggers {
		if em.shouldTrigger(trigger, data) {
			em.executeTrigger(trigger)
		}
	}
}

func (em *EventManager) shouldTrigger(trigger *EventTrigger, data map[string]interface{}) bool {
	// Check debounce
	if trigger.Debounce > 0 && time.Since(trigger.LastFired) < trigger.Debounce {
		return false
	}

	// Check filter
	if !em.matchesFilter(trigger.Filter, data) {
		return false
	}

	// Check conditions
	if len(trigger.Conditions) > 0 {
		conditionalEngine := NewConditionalEngine(em.scheduler)
		result, err := conditionalEngine.EvaluateConditions(trigger.Conditions, data)
		if err != nil || !result {
			return false
		}
	}

	return true
}

func (em *EventManager) matchesFilter(filter map[string]interface{}, data map[string]interface{}) bool {
	for key, expectedValue := range filter {
		if actualValue, exists := data[key]; !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}

func (em *EventManager) executeTrigger(trigger *EventTrigger) {
	trigger.LastFired = time.Now()

	for _, taskID := range trigger.Tasks {
		if task, err := em.scheduler.GetTask(taskID); err == nil {
			go em.scheduler.TriggerTask(task)
		}
	}
}

func (ce *ConditionEvaluator) toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
