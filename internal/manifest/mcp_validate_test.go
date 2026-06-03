package manifest

import (
	"strings"
	"testing"
)

// validMCPAgent returns a valid agent that declares one MCP server (bearer auth)
// and one MCP-backed tool binding, with the secrets the auth references.
func validMCPAgent() *Agent {
	a := validAgent()
	a.Secrets = map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		"mcp_token": {FromEnv: "MCP_TOKEN"},
	}
	a.Spec.MCPServers = []MCPServerSpec{{
		Name: "search",
		URL:  "https://mcp.example.com/mcp",
		Auth: &MCPServerAuth{Scheme: MCPAuthSchemeBearer, Secret: "mcp_token"},
	}}
	a.Spec.Tools = []ToolBinding{{
		Ref:             "search.web",
		InputSchema:     &SchemaSpec{Inline: map[string]any{"type": "object"}},
		SideEffectClass: SideEffectReadOnly,
		MCP:             &ToolMCPBinding{Server: "search"},
	}}
	return a
}

func TestValidate_mcpServersAndBindings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*Agent)
		wantPaths []string
	}{
		{
			name:      "valid mcp server and binding",
			mutate:    func(*Agent) {},
			wantPaths: nil,
		},
		{
			name: "explicit streamable_http transport is valid",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Transport = MCPTransportStreamableHTTP
			},
			wantPaths: nil,
		},
		{
			name: "header auth scheme with header is valid",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth = &MCPServerAuth{
					Scheme: MCPAuthSchemeHeader,
					Secret: "mcp_token",
					Header: "X-API-Key",
				}
			},
			wantPaths: nil,
		},
		{
			name: "server without auth is valid",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth = nil
			},
			wantPaths: nil,
		},
		{
			name: "remote tool override is valid",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].MCP.Tool = "web_search"
			},
			wantPaths: nil,
		},
		{
			name: "missing server name",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Name = ""
			},
			wantPaths: []string{"spec.mcp_servers[0].name"},
		},
		{
			name: "duplicate server names",
			mutate: func(a *Agent) {
				a.Spec.MCPServers = append(a.Spec.MCPServers, MCPServerSpec{
					Name: "search",
					URL:  "https://other.example.com/mcp",
				})
			},
			wantPaths: []string{"spec.mcp_servers[1].name"},
		},
		{
			name: "missing server url",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].URL = ""
			},
			wantPaths: []string{"spec.mcp_servers[0].url"},
		},
		{
			name: "non-https server url",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].URL = "http://mcp.example.com/mcp"
			},
			wantPaths: []string{"spec.mcp_servers[0].url"},
		},
		{
			name: "invalid transport",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Transport = "stdio"
			},
			wantPaths: []string{"spec.mcp_servers[0].transport"},
		},
		{
			name: "auth scheme missing",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth.Scheme = ""
			},
			wantPaths: []string{"spec.mcp_servers[0].auth.scheme"},
		},
		{
			name: "auth scheme invalid",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth.Scheme = "oauth"
			},
			wantPaths: []string{"spec.mcp_servers[0].auth.scheme"},
		},
		{
			name: "auth secret not declared",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth.Secret = "missing"
			},
			wantPaths: []string{"spec.mcp_servers[0].auth.secret"},
		},
		{
			name: "auth secret missing",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth.Secret = ""
			},
			wantPaths: []string{"spec.mcp_servers[0].auth.secret"},
		},
		{
			name: "header scheme without header name",
			mutate: func(a *Agent) {
				a.Spec.MCPServers[0].Auth = &MCPServerAuth{
					Scheme: MCPAuthSchemeHeader,
					Secret: "mcp_token",
				}
			},
			wantPaths: []string{"spec.mcp_servers[0].auth.header"},
		},
		{
			name: "binding references undeclared server",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].MCP.Server = "unknown"
			},
			wantPaths: []string{"spec.tools[0].mcp.server"},
		},
		{
			name: "binding missing server",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].MCP.Server = ""
			},
			wantPaths: []string{"spec.tools[0].mcp.server"},
		},
		{
			name: "binding missing input_schema",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].InputSchema = nil
			},
			wantPaths: []string{"spec.tools[0].input_schema"},
		},
		{
			name: "binding missing side_effect_class",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].SideEffectClass = ""
			},
			wantPaths: []string{"spec.tools[0].side_effect_class"},
		},
		{
			name: "binding ref pins a version constraint",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Ref = "search.web@1.0.0"
			},
			wantPaths: []string{"spec.tools[0].ref"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validMCPAgent()
			tc.mutate(agent)
			err := Validate(agent)
			if len(tc.wantPaths) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			valErrs, ok := err.(ValidationErrors)
			if !ok {
				t.Fatalf("error type %T, want ValidationErrors", err)
			}
			msg := err.Error()
			for _, path := range tc.wantPaths {
				if !pathInErrors(valErrs, path) && !strings.Contains(msg, path) {
					t.Fatalf("error %q missing path %q; got %v", msg, path, valErrs)
				}
			}
		})
	}
}

func TestToolBinding_mcpAccessors(t *testing.T) {
	t.Parallel()

	worker := ToolBinding{Ref: "weather.get-forecast"}
	if worker.IsMCP() {
		t.Fatal("worker binding should not be MCP")
	}
	if worker.MCPToolName() != "" {
		t.Fatalf("worker MCPToolName() = %q, want empty", worker.MCPToolName())
	}

	defaulted := ToolBinding{Ref: "search.web", As: "web_search", MCP: &ToolMCPBinding{Server: "search"}}
	if !defaulted.IsMCP() {
		t.Fatal("MCP binding should report IsMCP")
	}
	if got := defaulted.MCPToolName(); got != "web_search" {
		t.Fatalf("MCPToolName() = %q, want web_search (wire name default)", got)
	}
	if got := defaulted.DispatchRef(); got != "search.web" {
		t.Fatalf("DispatchRef() = %q, want search.web", got)
	}

	override := ToolBinding{Ref: "search.web", MCP: &ToolMCPBinding{Server: "search", Tool: "remote_search"}}
	if got := override.MCPToolName(); got != "remote_search" {
		t.Fatalf("MCPToolName() = %q, want remote_search", got)
	}
}
