package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

// TestParseValidate_mcpBundle proves an MCP-backed agent parses, validates, and
// exposes its mcp_servers and MCP tool binding fields.
func TestParseValidate_mcpBundle(t *testing.T) {
	t.Parallel()
	data := readTestdataFile(t, filepath.Join("bundle-mcp", "agent.yaml"))
	agent, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := manifest.Validate(agent); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if len(agent.Spec.MCPServers) != 1 {
		t.Fatalf("mcp_servers len = %d, want 1", len(agent.Spec.MCPServers))
	}
	srv := agent.Spec.MCPServers[0]
	if srv.Name != "search" || srv.URL != "https://mcp.example.com/mcp" {
		t.Fatalf("mcp_server = %+v", srv)
	}
	if got := srv.ResolvedTransport(); got != manifest.MCPTransportStreamableHTTP {
		t.Fatalf("ResolvedTransport() = %q, want streamable_http", got)
	}
	if srv.Auth == nil || srv.Auth.Scheme != manifest.MCPAuthSchemeBearer || srv.Auth.Secret != "mcp_token" {
		t.Fatalf("mcp_server.auth = %+v", srv.Auth)
	}

	if len(agent.Spec.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(agent.Spec.Tools))
	}
	tb := agent.Spec.Tools[0]
	if !tb.IsMCP() {
		t.Fatal("tool binding should be MCP-backed")
	}
	if tb.MCP.Server != "search" {
		t.Fatalf("tool.mcp.server = %q, want search", tb.MCP.Server)
	}
	if got := tb.MCPToolName(); got != "web_search" {
		t.Fatalf("MCPToolName() = %q, want web_search", got)
	}
	if got := tb.DispatchRef(); got != "search.web" {
		t.Fatalf("DispatchRef() = %q, want search.web", got)
	}
}

// TestResolveBundle_mcpBindingSkipsCatalog proves an MCP binding resolves its
// schema ref from the bundle without needing a tools/ catalog entry, and that
// the server declaration is carried into the resolved snapshot.
func TestResolveBundle_mcpBindingSkipsCatalog(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("testdata", "bundle-mcp")
	agentPath := filepath.Join(bundle, "agent.yaml")

	data := readTestdataFile(t, filepath.Join("bundle-mcp", "agent.yaml"))
	agent, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolved, err := manifest.ResolveBundle(agentPath, agent)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}

	tb := resolved.Agent.Spec.Tools[0]
	if !tb.IsMCP() {
		t.Fatal("resolved tool binding should remain MCP-backed")
	}
	if tb.InputSchema == nil || tb.InputSchema.Ref != "" {
		t.Fatalf("input_schema not inlined: %+v", tb.InputSchema)
	}
	if tb.InputSchema.Inline["type"] != "object" {
		t.Fatalf("input_schema.inline = %v", tb.InputSchema.Inline)
	}
	if len(resolved.Agent.Spec.MCPServers) != 1 {
		t.Fatalf("resolved mcp_servers len = %d, want 1", len(resolved.Agent.Spec.MCPServers))
	}

	raw, err := resolved.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(raw), "schemas/web-search-input") {
		t.Fatalf("JSON() still contains unresolved schema ref: %s", string(raw))
	}
}

// TestCompile_mcpBundle proves an MCP tool's attached Policy is inlined into the
// compiled snapshot just like a worker tool, with the server carried through.
func TestCompile_mcpBundle(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("testdata", "bundle-mcp")
	agentPath := filepath.Join(bundle, "agent.yaml")

	data := readTestdataFile(t, filepath.Join("bundle-mcp", "agent.yaml"))
	agent, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := manifest.Validate(agent); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	resolved, err := manifest.Compile(agentPath, agent)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	tb := resolved.Agent.Spec.Tools[0]
	if !tb.IsMCP() {
		t.Fatal("compiled tool binding should remain MCP-backed")
	}
	if len(tb.Policies) != 0 {
		t.Fatalf("tool policy attachments = %v, want cleared after compile", tb.Policies)
	}
	if len(resolved.Agent.Spec.MCPServers) != 1 {
		t.Fatalf("compiled mcp_servers len = %d, want 1", len(resolved.Agent.Spec.MCPServers))
	}

	var found *manifest.PolicySpec
	for i := range resolved.Agent.Spec.Policies {
		p := &resolved.Agent.Spec.Policies[i]
		if p.Scope == "tool:search.web" && len(p.Allow) > 0 {
			found = p
			break
		}
	}
	if found == nil {
		t.Fatal("compiled allow policy for the MCP tool is missing")
	}
	if len(found.Allow) != 2 {
		t.Fatalf("allow list = %v, want weather and news", found.Allow)
	}
}
