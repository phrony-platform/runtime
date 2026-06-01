package manifest

import (
	"strings"
	"testing"
)

func TestParseJSON_success(t *testing.T) {
	raw := []byte(`{
		"apiVersion": "phrony.com/v1",
		"kind": "Agent",
		"metadata": {"name": "a", "namespace": "n", "version": "1.0.0"},
		"spec": {
			"purpose": "p",
			"instructions": {"text": "hi"},
			"model": {"provider": "anthropic", "name": "claude"}
		}
	}`)
	agent, err := ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if agent.Metadata.Name != "a" {
		t.Fatalf("name = %q, want a", agent.Metadata.Name)
	}
}

func TestParseJSON_invalidJSON(t *testing.T) {
	_, err := ParseJSON([]byte(`{`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("error = %v, want parse manifest prefix", err)
	}
}
