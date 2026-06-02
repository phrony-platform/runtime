package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelProviderStub is the dev-only scripted model provider id.
const ModelProviderStub = "stub"

// AnnotationStubScript holds the JSON stub script inlined at compile time.
const AnnotationStubScript = "phrony.com/stub-script"

const stubScriptFile = "stub-script.json"

// InlineStubScript reads stub-script.json from the bundle when spec.model.provider is stub.
func InlineStubScript(agentPath string, agent *Agent) error {
	if agent == nil || strings.TrimSpace(agent.Spec.Model.Provider) != ModelProviderStub {
		return nil
	}
	absAgent, err := filepath.Abs(agentPath)
	if err != nil {
		return fmt.Errorf("agent path: %w", err)
	}
	scriptPath := filepath.Join(filepath.Dir(absAgent), stubScriptFile)
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", stubScriptFile, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("%s is empty", stubScriptFile)
	}
	if agent.Metadata.Annotations == nil {
		agent.Metadata.Annotations = make(map[string]string)
	}
	agent.Metadata.Annotations[AnnotationStubScript] = string(data)
	return nil
}

// StubScriptFromAgent returns the inlined stub script JSON from a resolved agent snapshot.
func StubScriptFromAgent(agent *Agent) string {
	if agent == nil || agent.Metadata.Annotations == nil {
		return ""
	}
	return agent.Metadata.Annotations[AnnotationStubScript]
}
