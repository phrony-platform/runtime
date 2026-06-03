package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/phrony-platform/runtime/internal/mcp"
)

func TestClientCallToolSuccess(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	client := mcp.NewClient(mcp.ServerConfig{Name: "fake", URL: srv.URL})
	t.Cleanup(func() { _ = client.Close() })

	res, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"value":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	got, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if string(got) != `{"echo":"hi"}` {
		t.Fatalf("structured content = %s, want {\"echo\":\"hi\"}", got)
	}
}

func TestClientReusesSession(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	client := mcp.NewClient(mcp.ServerConfig{Name: "fake", URL: srv.URL})
	t.Cleanup(func() { _ = client.Close() })

	for i := 0; i < 3; i++ {
		if _, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"value":"x"}`)); err != nil {
			t.Fatalf("CallTool #%d: %v", i, err)
		}
	}
}

func TestClientInjectsAuthHeader(t *testing.T) {
	var seen string
	srv := newFakeMCPServer(t, func(r *http.Request) bool {
		if h := r.Header.Get("Authorization"); h != "" {
			seen = h
		}
		return r.Header.Get("Authorization") == "Bearer secret-token"
	})

	client := mcp.NewClient(mcp.ServerConfig{
		Name:    "fake",
		URL:     srv.URL,
		Headers: map[string]string{"Authorization": "Bearer secret-token"},
	})
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"value":"hi"}`)); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if seen != "Bearer secret-token" {
		t.Fatalf("auth header = %q, want %q", seen, "Bearer secret-token")
	}
}

func TestClientMissingAuthHeaderRejected(t *testing.T) {
	srv := newFakeMCPServer(t, func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer secret-token"
	})

	// No headers configured: the server rejects the handshake.
	client := mcp.NewClient(mcp.ServerConfig{Name: "fake", URL: srv.URL})
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"value":"hi"}`)); err == nil {
		t.Fatal("expected error when auth header is missing")
	}
}
