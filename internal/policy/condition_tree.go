package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// FieldDispatchTrigger matches policy conditions against the active dispatch failure trigger.
	FieldDispatchTrigger = "phrony.dispatch.trigger"
	// FieldDispatchOutcome is a shorthand alias; value indeterminate maps to dispatch:indeterminate.
	FieldDispatchOutcome = "dispatch.outcome"
)

// evaluateConditions reports whether a policy condition tree matches tool arguments
// and optional evaluation context. An empty tree matches all invocations.
func evaluateConditions(conditions map[string]any, args json.RawMessage, ctx EvalContext) bool {
	if len(conditions) == 0 {
		return true
	}
	return evalConditionNode(conditions, args, ctx)
}

func evalConditionNode(node map[string]any, args json.RawMessage, ctx EvalContext) bool {
	if node == nil {
		return true
	}
	if all, ok := node["all"]; ok {
		return evalConditionList(all, args, ctx, true)
	}
	if any, ok := node["any"]; ok {
		return evalConditionList(any, args, ctx, false)
	}
	if not, ok := node["not"]; ok {
		child, ok := not.(map[string]any)
		if !ok {
			return false
		}
		return !evalConditionNode(child, args, ctx)
	}
	if field, ok := node["field"].(string); ok && strings.TrimSpace(field) != "" {
		return evalLeafCondition(field, node, args, ctx)
	}
	return false
}

func evalConditionList(raw any, args json.RawMessage, ctx EvalContext, requireAll bool) bool {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return requireAll
	}
	if requireAll {
		for _, item := range items {
			child, ok := item.(map[string]any)
			if !ok || !evalConditionNode(child, args, ctx) {
				return false
			}
		}
		return true
	}
	for _, item := range items {
		child, ok := item.(map[string]any)
		if ok && evalConditionNode(child, args, ctx) {
			return true
		}
	}
	return false
}

func evalLeafCondition(field string, node map[string]any, args json.RawMessage, ctx EvalContext) bool {
	field = strings.TrimSpace(field)
	switch field {
	case FieldDispatchTrigger:
		return compareDispatchTrigger(ctx.DispatchTrigger, node)
	case FieldDispatchOutcome:
		return compareDispatchOutcome(ctx.DispatchTrigger, node)
	default:
		return evalArgLeafCondition(field, node, args)
	}
}

func compareDispatchTrigger(actual string, node map[string]any) bool {
	if strings.TrimSpace(actual) == "" {
		return false
	}
	op, _ := node["op"].(string)
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "" {
		op = "eq"
	}
	switch op {
	case "eq", "==":
		return compareEqual(actual, node["value"])
	case "neq", "!=":
		return !compareEqual(actual, node["value"])
	default:
		return false
	}
}

func compareDispatchOutcome(actualTrigger string, node map[string]any) bool {
	op, _ := node["op"].(string)
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "" {
		op = "eq"
	}
	want := strings.TrimSpace(valueString(node["value"]))
	if want == "indeterminate" {
		want = TriggerDispatchIndeterminate
	} else if want != "" && !strings.Contains(want, ":") {
		want = "dispatch:" + want
	}
	switch op {
	case "eq", "==":
		return compareEqual(actualTrigger, want)
	case "neq", "!=":
		return !compareEqual(actualTrigger, want)
	default:
		return false
	}
}

func evalArgLeafCondition(field string, node map[string]any, args json.RawMessage) bool {
	op, _ := node["op"].(string)
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "" {
		op = "eq"
	}
	actual, ok := argValueAtPath(args, field)
	if !ok {
		return false
	}
	switch op {
	case "eq", "==":
		return compareEqual(actual, node["value"])
	case "neq", "!=":
		return !compareEqual(actual, node["value"])
	case "gt", ">":
		return compareNumeric(actual, node["value"], func(a, b float64) bool { return a > b })
	case "gte", ">=":
		return compareNumeric(actual, node["value"], func(a, b float64) bool { return a >= b })
	case "lt", "<":
		return compareNumeric(actual, node["value"], func(a, b float64) bool { return a < b })
	case "lte", "<=":
		return compareNumeric(actual, node["value"], func(a, b float64) bool { return a <= b })
	case "in":
		return valueIn(actual, node["value"])
	case "matches":
		pattern, ok := node["value"].(string)
		if !ok {
			return false
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(valueString(actual))
	default:
		return false
	}
}

func compareEqual(actual any, expected any) bool {
	if fActual, ok := toFloat64(actual); ok {
		if fExpected, ok := toFloat64(expected); ok {
			return fActual == fExpected
		}
	}
	return strings.EqualFold(valueString(actual), valueString(expected))
}

func compareNumeric(actual, expected any, cmp func(float64, float64) bool) bool {
	a, okA := toFloat64(actual)
	b, okB := toFloat64(expected)
	if !okA || !okB {
		return false
	}
	return cmp(a, b)
}

func valueIn(actual any, list any) bool {
	items, ok := list.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if compareEqual(actual, item) {
			return true
		}
	}
	return false
}

func argValueAtPath(args json.RawMessage, path string) (any, bool) {
	if len(args) == 0 || strings.TrimSpace(path) == "" {
		return nil, false
	}
	var root any
	if err := json.Unmarshal(args, &root); err != nil {
		return nil, false
	}
	cur := root
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[part]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// allowValueFromArgs picks the first string-like field used for allow-list checks.
func allowValueFromArgs(args json.RawMessage) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	var obj map[string]any
	if err := json.Unmarshal(args, &obj); err != nil {
		return "", false
	}
	for _, key := range []string{"queue", "target", "value", "name", "id", "currency"} {
		if v, ok := obj[key]; ok {
			if s, ok := stringifyAllowValue(v); ok {
				return s, true
			}
		}
	}
	for _, v := range obj {
		if s, ok := stringifyAllowValue(v); ok {
			return s, true
		}
	}
	return "", false
}

func stringifyAllowValue(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		return s, s != ""
	case float64:
		return fmt.Sprintf("%v", t), true
	default:
		return "", false
	}
}

func valueString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return fmt.Sprintf("%v", t)
	case bool:
		return strconv.FormatBool(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
