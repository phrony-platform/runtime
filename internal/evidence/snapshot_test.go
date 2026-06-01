package evidence_test

import (
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/evidence"
	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestBuildSnapshot_descriptiveAndFrameworks(t *testing.T) {
	t.Parallel()
	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Owner: "claims-platform",
			Labels: map[string]string{
				"owning-team": "claims",
			},
			Annotations: map[string]string{
				"cost-center": "CC-4471",
			},
			Governance: &manifest.GovernanceMetadata{
				RiskTier: "high",
				AuthorityBoundaries: []string{
					"claims.payment-authority",
				},
				Classifications: []string{"claims.high-impact-financial"},
				Frameworks: map[string]json.RawMessage{
					"eu-ai-act/v1":          json.RawMessage(`{"role":"provider","annex_iii_category":"credit-scoring"}`),
					"custom-regime/v1":      json.RawMessage(`{"note":"internal"}`),
					"nist-ai-rmf/v1":   json.RawMessage(`{"role":"bad"}`),
				},
			},
		},
	}

	snap := evidence.BuildSnapshot(agent)
	if snap.Owner != "claims-platform" {
		t.Fatalf("owner = %q", snap.Owner)
	}
	if snap.Labels["owning-team"] != "claims" {
		t.Fatalf("labels = %v", snap.Labels)
	}
	if snap.Annotations["cost-center"] != "CC-4471" {
		t.Fatalf("annotations = %v", snap.Annotations)
	}
	if snap.Governance == nil {
		t.Fatal("governance missing")
	}
	if snap.Governance.RiskTier != "high" {
		t.Fatalf("risk_tier = %q", snap.Governance.RiskTier)
	}
	if len(snap.Governance.Frameworks) != 3 {
		t.Fatalf("frameworks = %d, want 3 (all packs carried)", len(snap.Governance.Frameworks))
	}
	byID := make(map[string]evidence.FrameworkPack, len(snap.Governance.Frameworks))
	for _, fw := range snap.Governance.Frameworks {
		byID[fw.ID] = fw
	}
	if byID["eu-ai-act/v1"].Validation != evidence.ValidationValidated {
		t.Fatalf("eu-ai-act validation = %q", byID["eu-ai-act/v1"].Validation)
	}
	if byID["custom-regime/v1"].Validation != evidence.ValidationUnvalidated {
		t.Fatalf("custom-regime validation = %q", byID["custom-regime/v1"].Validation)
	}
	if string(byID["custom-regime/v1"].Payload) != `{"note":"internal"}` {
		t.Fatalf("custom-regime payload = %s", byID["custom-regime/v1"].Payload)
	}
	if byID["nist-ai-rmf/v1"].Validation != evidence.ValidationUnvalidated {
		t.Fatalf("unknown pack validation = %q", byID["nist-ai-rmf/v1"].Validation)
	}
}

func TestBuildSnapshot_emptyPayloadFramework(t *testing.T) {
	t.Parallel()
	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Governance: &manifest.GovernanceMetadata{
				Frameworks: map[string]json.RawMessage{
					"unknown/v1": nil,
				},
			},
		},
	}
	snap := evidence.BuildSnapshot(agent)
	if len(snap.Governance.Frameworks) != 1 {
		t.Fatalf("frameworks = %v", snap.Governance.Frameworks)
	}
	if string(snap.Governance.Frameworks[0].Payload) != "{}" {
		t.Fatalf("payload = %s", snap.Governance.Frameworks[0].Payload)
	}
}
