package policy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// conditionMatches reports whether manifest HITL/policy conditions hold for args.
// Supports comparisons on numeric JSON fields, e.g. "severity >= 3".
func conditionMatches(condition string, args json.RawMessage) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}
	field, op, threshold, ok := parseCondition(condition)
	if !ok {
		return false
	}
	value, ok := numericArgField(args, field)
	if !ok {
		return false
	}
	switch op {
	case ">=":
		return value >= threshold
	case ">":
		return value > threshold
	case "<=":
		return value <= threshold
	case "<":
		return value < threshold
	case "==", "=":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

func parseCondition(condition string) (field, op string, threshold float64, ok bool) {
	for _, candidate := range []string{">=", "<=", "!=", "==", ">", "<", "="} {
		if i := strings.Index(condition, candidate); i > 0 {
			field = strings.TrimSpace(condition[:i])
			op = candidate
			if op == "=" {
				op = "=="
			}
			numStr := strings.TrimSpace(condition[i+len(candidate):])
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil || field == "" {
				return "", "", 0, false
			}
			return field, op, v, true
		}
	}
	return "", "", 0, false
}

func numericArgField(args json.RawMessage, field string) (float64, bool) {
	if len(args) == 0 || field == "" {
		return 0, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return 0, false
	}
	raw, ok := obj[field]
	if !ok {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil
	}
	return 0, false
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
	for _, key := range []string{"queue", "target", "value", "name", "id"} {
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
