package manifest

import "testing"

func TestParse_rejectsForbiddenKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "hitl",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  hitl:
    - trigger: dispatch:indeterminate
`,
		},
		{
			name: "embedded policies",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  policies:
    - name: x
      action: deny
`,
		},
		{
			name: "tool name",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      name: wire
`,
		},
		{
			name: "tool parameters",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      parameters:
        inline: {type: object}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tc.yaml)); err == nil {
				t.Fatalf("Parse() = nil, want error for %s", tc.name)
			}
		})
	}
}
