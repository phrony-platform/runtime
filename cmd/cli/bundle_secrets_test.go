package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc"
)

type bundleSecretsRuntimeClient struct {
	recordingRuntimeClient
	requirements *runtimev1.GetBundleSecretRequirementsResponse
}

func (c *bundleSecretsRuntimeClient) GetBundleSecretRequirements(context.Context, *runtimev1.GetBundleSecretRequirementsRequest, ...grpc.CallOption) (*runtimev1.GetBundleSecretRequirementsResponse, error) {
	return c.requirements, nil
}

func TestPrepareBundleRunSecrets_readsFromRuntimeUnion(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic")

	rt := &bundleSecretsRuntimeClient{
		requirements: &runtimev1.GetBundleSecretRequirementsResponse{
			Secrets: map[string]*runtimev1.SecretRequirement{
				"openai":    {FromEnv: "OPENAI_API_KEY"},
				"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
			},
		},
	}
	got, err := prepareBundleRunSecrets(context.Background(), rt, &runtimev1.BundleRef{
		Namespace: "demo",
		Name:      "routing",
	}, nil)
	if err != nil {
		t.Fatalf("prepareBundleRunSecrets: %v", err)
	}
	if string(got["openai"]) != "sk-openai" {
		t.Fatalf("openai = %q", got["openai"])
	}
	if string(got["anthropic"]) != "sk-anthropic" {
		t.Fatalf("anthropic = %q", got["anthropic"])
	}
}

func TestPrepareBundleRunSecrets_missingEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	rt := &bundleSecretsRuntimeClient{
		requirements: &runtimev1.GetBundleSecretRequirementsResponse{
			Secrets: map[string]*runtimev1.SecretRequirement{
				"openai": {FromEnv: "OPENAI_API_KEY"},
			},
		},
	}
	_, err := prepareBundleRunSecrets(context.Background(), rt, &runtimev1.BundleRef{
		Namespace: "demo",
		Name:      "routing",
	}, nil)
	if err == nil {
		t.Fatal("expected missing env error")
	}
}

func TestPrepareBundleRunSecrets_readsFromEnvFile(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=sk-from-env-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rt := &bundleSecretsRuntimeClient{
		requirements: &runtimev1.GetBundleSecretRequirementsResponse{
			Secrets: map[string]*runtimev1.SecretRequirement{
				"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
			},
		},
	}
	got, err := prepareBundleRunSecrets(context.Background(), rt, &runtimev1.BundleRef{
		Namespace: "demo",
		Name:      "routing",
	}, []string{envPath})
	if err != nil {
		t.Fatalf("prepareBundleRunSecrets: %v", err)
	}
	if string(got["anthropic"]) != "sk-from-env-file" {
		t.Fatalf("anthropic = %q", got["anthropic"])
	}
}
