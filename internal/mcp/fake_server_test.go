package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/phrony-platform/runtime/internal/mcp/mcptest"
)

// newFakeMCPServer starts the shared mcptest fake MCP server (an "echo" tool
// returning structured output and a "boom" tool returning an MCP tool error).
// The optional inspect hook is invoked for every request before the MCP handler
// runs (e.g. to assert auth headers); returning false writes 401.
func newFakeMCPServer(t *testing.T, inspect func(*http.Request) bool) *httptest.Server {
	t.Helper()
	return mcptest.NewServer(t, inspect)
}
