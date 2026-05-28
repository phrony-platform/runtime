package manifest

import (
	"fmt"
	"strings"
)

// FieldError is a single validation failure at a dotted field path.
type FieldError struct {
	Path    string
	Message string
}

func (e FieldError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

// ValidationErrors aggregates field-level validation failures.
type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "invalid manifest"
	}
	if len(v) == 1 {
		return v[0].Error()
	}
	msgs := make([]string, len(v))
	for i, e := range v {
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("invalid manifest (%d errors):\n  %s", len(v), strings.Join(msgs, "\n  "))
}

func (v ValidationErrors) Unwrap() error {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}
