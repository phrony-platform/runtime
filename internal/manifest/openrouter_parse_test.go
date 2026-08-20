package manifest

import "testing"

func TestParse_openrouterBaseURL(t *testing.T) {
	yaml := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: a
  namespace: ns
  version: 1.0.0
secrets:
  openrouter:
    fromEnv: OPENROUTER_API_KEY
spec:
  purpose: p
  instructions:
    text: hi
  model:
    provider: openai-compatible
    base_url: https://openrouter.ai
    name: gpt-4o
    secret: openrouter
`
	agent, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if agent.Spec.Model.BaseURL != "https://openrouter.ai" {
		t.Fatalf("BaseURL = %q, want https://openrouter.ai", agent.Spec.Model.BaseURL)
	}
	if err := Validate(agent); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	raw, err := (&ResolvedAgent{Agent: agent}).JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	roundTrip, err := ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if roundTrip.Spec.Model.BaseURL == "" {
		t.Fatalf("BaseURL lost after JSON round-trip: %s", string(raw))
	}
}

func TestValidate_baseURLOnSpecNotModel(t *testing.T) {
	yaml := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: a
  namespace: ns
  version: 1.0.0
spec:
  purpose: p
  instructions:
    text: hi
  model:
    provider: openai-compatible
    name: gpt-4o
  base_url: https://openrouter.ai
`
	agent, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if agent.Spec.Model.BaseURL != "" {
		t.Fatalf("model BaseURL = %q, want empty when base_url is under spec", agent.Spec.Model.BaseURL)
	}
	err = Validate(agent)
	if err == nil {
		t.Fatal("Validate() = nil, want base_url required error")
	}
	if !pathInErrors(err.(ValidationErrors), "spec.model.base_url") {
		t.Fatalf("Validate() = %v, want spec.model.base_url error", err)
	}
}
