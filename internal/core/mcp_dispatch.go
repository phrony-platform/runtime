package core

import (
	"context"
	"fmt"

	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/mcp"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// sessionToolDispatch resolves the tool dispatcher for a session's agent
// version. When the version declares spec.mcp_servers with MCP-backed tool
// bindings, it returns a routing dispatcher that sends those tools to a native
// MCP client and falls through to the worker registry (s.toolDispatch) for
// everything else. Versions without MCP tools reuse the shared worker
// dispatcher unchanged, so the common path allocates nothing extra.
//
// The returned dispatcher may hold MCP sessions; callers that drive turns
// should closeSessionDispatch it when the session work is done.
func (s *runtimeServer) sessionToolDispatch(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	ver *executor.Version,
) (tooldispatch.Dispatcher, error) {
	if ver == nil || ver.Agent == nil || len(ver.Agent.Spec.MCPServers) == 0 {
		return s.toolDispatch, nil
	}
	agent := ver.Agent

	bindings := make(map[string]mcp.Binding)
	usedServers := make(map[string]bool)
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if !tb.IsMCP() {
			continue
		}
		bindings[tb.DispatchRef()] = mcp.Binding{
			Server:     tb.MCP.Server,
			RemoteTool: tb.MCPToolName(),
		}
		usedServers[tb.MCP.Server] = true
	}
	if len(bindings) == 0 {
		return s.toolDispatch, nil
	}

	clients := make(map[string]*mcp.Client)
	for i := range agent.Spec.MCPServers {
		srv := agent.Spec.MCPServers[i]
		if !usedServers[srv.Name] {
			continue
		}
		headers, err := s.mcpAuthHeaders(ctx, q, sessionID, srv)
		if err != nil {
			return nil, err
		}
		clients[srv.Name] = mcp.NewClient(mcp.ServerConfig{
			Name:    srv.Name,
			URL:     srv.URL,
			Headers: headers,
		})
	}

	disp := mcp.NewDispatcher(clients, bindings)
	disp.SetInvocationRecorder(NewToolInvocationRecorder(q))
	return &tooldispatch.RoutingDispatcher{Primary: disp, Fallback: s.toolDispatch}, nil
}

// mcpAuthHeaders resolves the static request headers for an MCP server from its
// auth scheme, decrypting the named Phrony secret for this session.
func (s *runtimeServer) mcpAuthHeaders(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	srv manifest.MCPServerSpec,
) (map[string]string, error) {
	if srv.Auth == nil {
		return nil, nil
	}
	secret, err := s.secretsEnc.DecryptForSession(ctx, q, sessionID, srv.Auth.Secret)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: decrypt auth secret %q: %w", srv.Name, srv.Auth.Secret, err)
	}
	value := string(secret)
	switch srv.Auth.Scheme {
	case manifest.MCPAuthSchemeBearer:
		return map[string]string{"Authorization": "Bearer " + value}, nil
	case manifest.MCPAuthSchemeHeader:
		return map[string]string{srv.Auth.Header: value}, nil
	default:
		return nil, fmt.Errorf("mcp server %q: unsupported auth scheme %q", srv.Name, srv.Auth.Scheme)
	}
}

// closeSessionDispatch releases per-session dispatcher resources. It only closes
// routing dispatchers built per session; the shared worker dispatcher returned
// for non-MCP agents is left open for reuse across sessions.
func closeSessionDispatch(d tooldispatch.Dispatcher) {
	if r, ok := d.(*tooldispatch.RoutingDispatcher); ok {
		_ = r.Close()
	}
}
