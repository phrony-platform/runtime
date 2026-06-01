package tooldispatch

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const envToolAllowlist = "RUNTIME_TOOL_ALLOWLIST"

// AllowlistEntry is one approved worker binding for (agent, tool, version).
// Agent is namespace/name (e.g. demo/echo). Tool is the manifest ref, not the wire name.
type AllowlistEntry struct {
	Agent              string   `yaml:"agent"`
	Tool               string   `yaml:"tool"`
	Version            string   `yaml:"version"`
	ContractVersion    string   `yaml:"contract_version"`
	WorkloadIdentities []string `yaml:"workload_identities"`
	ImageDigests       []string `yaml:"image_digests"`
}

// AllowlistConfig is the deployment/runtime allowlist document (YAML).
type AllowlistConfig struct {
	Entries []AllowlistEntry `yaml:"entries"`
}

// Allowlist indexes entries for dispatch-time integrity checks.
type Allowlist struct {
	byKey map[string]AllowlistEntry
}

func allowlistKey(agent, tool, version string) string {
	return agent + "|" + tool + "|" + version
}

// LoadAllowlistFromEnv loads RUNTIME_TOOL_ALLOWLIST when set. An empty path means no allowlist.
func LoadAllowlistFromEnv() (*Allowlist, error) {
	path := strings.TrimSpace(os.Getenv(envToolAllowlist))
	if path == "" {
		return nil, nil
	}
	return LoadAllowlistFile(path)
}

// LoadAllowlistFile parses a YAML allowlist document.
func LoadAllowlistFile(path string) (*Allowlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tool allowlist %q: %w", path, err)
	}
	var cfg AllowlistConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse tool allowlist %q: %w", path, err)
	}
	return NewAllowlist(cfg.Entries)
}

// NewAllowlist builds a lookup table from entries. Duplicate keys are rejected.
func NewAllowlist(entries []AllowlistEntry) (*Allowlist, error) {
	byKey := make(map[string]AllowlistEntry, len(entries))
	for _, e := range entries {
		agent := strings.TrimSpace(e.Agent)
		tool := strings.TrimSpace(e.Tool)
		version := normalizeToolVersion(e.Version)
		if agent == "" || tool == "" {
			return nil, fmt.Errorf("allowlist entry requires agent and tool")
		}
		key := allowlistKey(agent, tool, version)
		if _, exists := byKey[key]; exists {
			return nil, fmt.Errorf("duplicate allowlist entry for %s", key)
		}
		byKey[key] = AllowlistEntry{
			Agent:              agent,
			Tool:               tool,
			Version:            version,
			ContractVersion:    strings.TrimSpace(e.ContractVersion),
			WorkloadIdentities: e.WorkloadIdentities,
			ImageDigests:       e.ImageDigests,
		}
	}
	return &Allowlist{byKey: byKey}, nil
}

func normalizeToolVersion(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "default"
	}
	return v
}

// Lookup returns the entry for agent/tool@version, if present.
func (a *Allowlist) Lookup(agentKey, tool, version string) (AllowlistEntry, bool) {
	if a == nil || len(a.byKey) == 0 {
		return AllowlistEntry{}, false
	}
	entry, ok := a.byKey[allowlistKey(strings.TrimSpace(agentKey), strings.TrimSpace(tool), normalizeToolVersion(version))]
	return entry, ok
}

// Checker returns an IntegrityCheck that enforces the allowlist. When the allowlist
// is nil or has no entries, all workers pass.
func (a *Allowlist) Checker() IntegrityCheck {
	if a == nil || len(a.byKey) == 0 {
		return nil
	}
	return func(call ToolCall, worker *WorkerInfo) error {
		return a.check(call, worker)
	}
}

func (a *Allowlist) check(call ToolCall, worker *WorkerInfo) error {
	if worker == nil {
		return &IntegrityError{
			Violation: IntegrityWorkloadIdentity,
			Tool:      call.Tool,
			Version:   call.Version,
			Detail:    "worker info is required",
		}
	}
	entry, ok := a.Lookup(call.AgentKey, call.Tool, call.Version)
	if !ok {
		return &IntegrityError{
			Violation: IntegrityWorkloadIdentity,
			Tool:      call.Tool,
			Version:   call.Version,
			Detail:    fmt.Sprintf("agent %q is not on the tool allowlist", call.AgentKey),
		}
	}
	if !containsCI(entry.WorkloadIdentities, worker.WorkloadIdentity) {
		return &IntegrityError{
			Violation: IntegrityWorkloadIdentity,
			Tool:      call.Tool,
			Version:   call.Version,
			Detail:    fmt.Sprintf("workload identity %q is not approved", worker.WorkloadIdentity),
		}
	}
	if !containsCI(entry.ImageDigests, worker.ImageDigest) {
		return &IntegrityError{
			Violation: IntegrityImageDigest,
			Tool:      call.Tool,
			Version:   call.Version,
			Detail:    fmt.Sprintf("image digest %q is not approved", worker.ImageDigest),
		}
	}
	if entry.ContractVersion != "" {
		got := worker.ContractVersions[call.ToolKey()]
		if !strings.EqualFold(strings.TrimSpace(entry.ContractVersion), strings.TrimSpace(got)) {
			return &IntegrityError{
				Violation: IntegrityContractVersion,
				Tool:      call.Tool,
				Version:   call.Version,
				Detail:    fmt.Sprintf("contract version %q does not match required %q", got, entry.ContractVersion),
			}
		}
	}
	return nil
}

func containsCI(allowed []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), value) {
			return true
		}
	}
	return false
}
