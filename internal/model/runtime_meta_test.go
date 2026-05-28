package model

import "testing"

func TestRuntimeMeta_TableName(t *testing.T) {
	if got := (RuntimeMeta{}).TableName(); got != "runtime_meta" {
		t.Fatalf("TableName() = %q, want runtime_meta", got)
	}
}
