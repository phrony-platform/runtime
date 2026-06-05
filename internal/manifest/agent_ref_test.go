package manifest

import "testing"

func TestParseAgentEdgeRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		ref       string
		lateBound bool
		wantKind  AgentEdgeRefKind
		wantErr   bool
	}{
		{
			name:     "local path",
			ref:      "./specialists/billing.yaml",
			wantKind: AgentEdgeRefKindLocal,
		},
		{
			name:     "pinned external",
			ref:      "support.billing-specialist@1.2.0",
			wantKind: AgentEdgeRefKindExternal,
		},
		{
			name:      "floating external with late_bound",
			ref:       "support.billing-specialist",
			lateBound: true,
			wantKind:  AgentEdgeRefKindExternal,
		},
		{
			name:    "invalid ref",
			ref:     "not-a-ref",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			edge, err := ParseAgentEdgeRef(tc.ref, tc.lateBound)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ParseAgentEdgeRef() = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAgentEdgeRef() = %v", err)
			}
			if edge.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", edge.Kind, tc.wantKind)
			}
		})
	}
}

func TestIsLocalAgentRef_and_IsExternalAgentRef(t *testing.T) {
	t.Parallel()
	if !IsLocalAgentRef("./orchestrator.yaml") {
		t.Fatal("expected local path")
	}
	if IsExternalAgentRef("./orchestrator.yaml") {
		t.Fatal("local path must not be external")
	}
	if !IsExternalAgentRef("support.billing@1.0.0") {
		t.Fatal("expected external ref")
	}
	if IsLocalAgentRef("support.billing@1.0.0") {
		t.Fatal("external ref must not be local")
	}
}
