package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"google.golang.org/grpc"
)

type secretsRuntimeClient struct {
	recordingRuntimeClient
	activeVersion string
	manifest      []byte
}

func (c *secretsRuntimeClient) GetActiveVersion(context.Context, *runtimev1.GetActiveVersionRequest, ...grpc.CallOption) (*runtimev1.GetActiveVersionResponse, error) {
	return &runtimev1.GetActiveVersionResponse{Version: c.activeVersion}, nil
}

func (c *secretsRuntimeClient) GetAgentVersion(context.Context, *runtimev1.GetAgentVersionRequest, ...grpc.CallOption) (*runtimev1.GetAgentVersionResponse, error) {
	return &runtimev1.GetAgentVersionResponse{Manifest: c.manifest}, nil
}

func TestResolveRunSecrets_readsFromEnv(t *testing.T) {
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "echo-agent", Namespace: "demo", Version: "1.2.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "anthropic", Name: "m"},
		},
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-run")

	rt := &secretsRuntimeClient{
		activeVersion: "1.2.0",
		manifest:      raw,
	}
	got, err := resolveRunSecrets(context.Background(), rt, &runtimev1.AgentRef{
		Namespace: "demo",
		Name:      "echo-agent",
	})
	if err != nil {
		t.Fatalf("resolveRunSecrets: %v", err)
	}
	if string(got["anthropic"]) != "sk-test-run" {
		t.Fatalf("anthropic secret = %q, want sk-test-run", got["anthropic"])
	}
}

func TestPrepareRunSecrets_readsFromEnvFile(t *testing.T) {
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "echo-agent", Namespace: "demo", Version: "1.2.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "anthropic", Name: "m"},
		},
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=sk-from-env-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rt := &secretsRuntimeClient{manifest: raw}
	got, err := prepareRunSecrets(context.Background(), rt, &runtimev1.AgentRef{
		Namespace: "demo",
		Name:      "echo-agent",
		Version:   "1.2.0",
	}, []string{envPath})
	if err != nil {
		t.Fatalf("prepareRunSecrets: %v", err)
	}
	if string(got["anthropic"]) != "sk-from-env-file" {
		t.Fatalf("anthropic secret = %q, want sk-from-env-file", got["anthropic"])
	}
}

func TestResolveRunSecrets_missingEnv(t *testing.T) {
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "echo-agent", Namespace: "demo", Version: "1.2.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "anthropic", Name: "m"},
		},
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")

	rt := &secretsRuntimeClient{manifest: raw}
	_, err = resolveRunSecrets(context.Background(), rt, &runtimev1.AgentRef{
		Namespace: "demo",
		Name:      "echo-agent",
		Version:   "1.2.0",
	})
	if err == nil {
		t.Fatal("expected error for missing env, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "retry run") || !strings.Contains(msg, "ANTHROPIC_API_KEY") {
		t.Fatalf("error = %v, want missing env with retry run", err)
	}
}
