package core

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func mcpDispatchVersion(agent *manifest.Agent) *executor.Version {
	return executor.NewVersionWithProvider("av-1", agent, nil)
}

func mcpDispatchAgent() *manifest.Agent {
	return &manifest.Agent{
		Metadata: manifest.AgentMetadata{Namespace: "e2e", Name: "mcp-agent"},
		Spec: manifest.AgentSpec{
			MCPServers: []manifest.MCPServerSpec{{
				Name: "search-server",
				URL:  "https://mcp.example.com/mcp",
				Auth: &manifest.MCPServerAuth{Scheme: manifest.MCPAuthSchemeBearer, Secret: "mcp_token"},
			}},
			Tools: []manifest.ToolBinding{{
				Ref:             "search.web",
				SideEffectClass: manifest.SideEffectReadOnly,
				InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
				MCP:             &manifest.ToolMCPBinding{Server: "search-server"},
			}},
		},
	}
}

func TestSessionToolDispatch_noMCPReturnsWorkerDispatcher(t *testing.T) {
	worker := &tooldispatch.FakeDispatcher{}
	srv := &runtimeServer{toolDispatch: worker}

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Tools: []manifest.ToolBinding{{Ref: "weather.get-forecast"}},
		},
	}
	got, err := srv.sessionToolDispatch(context.Background(), nil, "sess-1", mcpDispatchVersion(agent), rootSessionDepth)
	if err != nil {
		t.Fatalf("sessionToolDispatch: %v", err)
	}
	if got != tooldispatch.Dispatcher(worker) {
		t.Fatalf("expected worker dispatcher unchanged, got %T", got)
	}
}

func TestSessionToolDispatch_mcpServerButNoMCPToolsReturnsWorkerDispatcher(t *testing.T) {
	worker := &tooldispatch.FakeDispatcher{}
	srv := &runtimeServer{toolDispatch: worker}

	agent := mcpDispatchAgent()
	agent.Spec.Tools = []manifest.ToolBinding{{Ref: "weather.get-forecast"}}

	got, err := srv.sessionToolDispatch(context.Background(), nil, "sess-1", mcpDispatchVersion(agent), rootSessionDepth)
	if err != nil {
		t.Fatalf("sessionToolDispatch: %v", err)
	}
	if got != tooldispatch.Dispatcher(worker) {
		t.Fatalf("expected worker dispatcher unchanged, got %T", got)
	}
}

func TestSessionToolDispatch_buildsRoutingDispatcherForMCPTool(t *testing.T) {
	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	worker := &tooldispatch.FakeDispatcher{}
	srv := &runtimeServer{db: db, secretsEnc: enc, toolDispatch: worker}

	sealed, err := enc.Encrypt("sess-1", "mcp_token", []byte("secret-token"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-1", "mcp_token").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	got, err := srv.sessionToolDispatch(context.Background(), store.New(db), "sess-1", mcpDispatchVersion(mcpDispatchAgent()), rootSessionDepth)
	if err != nil {
		t.Fatalf("sessionToolDispatch: %v", err)
	}
	routing, ok := got.(*tooldispatch.RoutingDispatcher)
	if !ok {
		t.Fatalf("expected *RoutingDispatcher, got %T", got)
	}
	if routing.Fallback != tooldispatch.Dispatcher(worker) {
		t.Fatal("routing fallback must be the worker dispatcher")
	}
	if !routing.Primary.Handles("search.web") {
		t.Fatal("routing primary should handle the MCP-backed tool ref")
	}
	if routing.Primary.Handles("weather.get-forecast") {
		t.Fatal("routing primary should not handle worker-backed tools")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSessionToolDispatch_secretDecryptErrorPropagates(t *testing.T) {
	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db, secretsEnc: enc, toolDispatch: &tooldispatch.FakeDispatcher{}}

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-1", "mcp_token").
		WillReturnError(context.DeadlineExceeded)

	_, err := srv.sessionToolDispatch(context.Background(), store.New(db), "sess-1", mcpDispatchVersion(mcpDispatchAgent()), rootSessionDepth)
	if err == nil {
		t.Fatal("expected error when auth secret cannot be decrypted")
	}
}

func TestSessionToolDispatch_unsupportedAuthScheme(t *testing.T) {
	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db, secretsEnc: enc, toolDispatch: &tooldispatch.FakeDispatcher{}}

	sealed, err := enc.Encrypt("sess-1", "mcp_token", []byte("secret-token"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-1", "mcp_token").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	agent := mcpDispatchAgent()
	agent.Spec.MCPServers[0].Auth.Scheme = "oauth"

	_, err = srv.sessionToolDispatch(context.Background(), store.New(db), "sess-1", mcpDispatchVersion(agent), rootSessionDepth)
	if err == nil {
		t.Fatal("expected error for unsupported auth scheme")
	}
}
