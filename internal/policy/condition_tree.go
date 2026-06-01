package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// evaluateConditions reports whether a policy condition tree matches tool arguments.
// An empty tree matches all invocations.
func evaluateConditions(conditions map[string]any, args json.RawMessage) bool {
	if len(conditions) == 0 {
		return true
	}
	return evalConditionNode(conditions, args)
}

func evalConditionNode(node map[string]any, args json.RawMessage) bool {
	if node == nil {
		return true
	}
	if all, ok := node["all"]; ok {
		return evalConditionList(all, args, true)
	}
	if any, ok := node["any"]; ok {
		return evalConditionList(any, args, false)
	}
	if not, ok := node["not"]; ok {
		child, ok := not.(map[string]any)
		if !ok {
			return false
		}
		return !evalConditionNode(child, args)
	}
	if field, ok := node["field"].(string); ok && strings.TrimSpace(field) != "" {
		return evalLeafCondition(field, node, args)
	}
	return false
}

func evalConditionList(raw any, args json.RawMessage, requireAll bool) bool {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return requireAll
	}
	if requireAll {
		for _, item := range items {
			child, ok := item.(map[string]any)
			if !ok || !evalConditionNode(child, args) {
				return false
			}
		}
		return true
	}
	for _, item := range items {
		child, ok := item.(map[string]any)
		if ok && evalConditionNode(child, args) {
			return true
		}
	}
	return false
}

func evalLeafCondition(field string, node map[string]any, args json.RawMessage) bool {
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
