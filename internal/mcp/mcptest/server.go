// Package mcptest provides a reusable fake MCP server for tests, backed by the
// real MCP SDK over Streamable HTTP. It exposes a small set of deterministic
// tools so client, dispatcher, and end-to-end tests can exercise the native
// MCP path without standing up a real server.
package mcptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// EchoInput is the argument shape for the "echo" tool.
type EchoInput struct {
	Value string `json:"value"`
}

// EchoOutput is the structured result of the "echo" tool.
type EchoOutput struct {
	Echo string `json:"echo"`
}

// ToolErrorMessage is the message returned by the "boom" tool's MCP tool error.
const ToolErrorMessage = "kaboom"

// NewServer starts an httptest server backed by a real MCP server over
// Streamable HTTP. It exposes:
//   - "echo": returns {"echo": <value>} structured output for input {"value": ...}.
//   - "boom": always returns an MCP tool error with message ToolErrorMessage.
//
// The optional inspect hook runs for every request before the MCP handler; when
// it returns false the server writes 401 and short-circuits (used to assert that
// auth headers are injected). The server is registered for cleanup on t.
func NewServer(t *testing.T, inspect func(*http.Request) bool) *httptest.Server {
	t.Helper()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake", Version: "v1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo", Description: "echo the input"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in EchoInput) (*mcpsdk.CallToolResult, EchoOutput, error) {
			return nil, EchoOutput{Echo: in.Value}, nil
		})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "boom", Description: "always fails"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{
				IsError: true,
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: ToolErrorMessage}},
			}, nil, nil
		})

	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inspect != nil && !inspect(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}
