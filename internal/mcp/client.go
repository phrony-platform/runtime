// Package mcp provides a native Model Context Protocol (MCP) client and a
// tooldispatch.Dispatcher that routes Phrony tool calls to remote MCP servers
// over Streamable HTTP. It mirrors the worker dispatch path: every call is
// gated by policy upstream and recorded in the tool invocation ledger, so MCP
// support reuses HITL approvals, recovery, and audit unchanged.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Phrony's identity in the MCP initialize handshake.
const (
	clientName    = "phrony-runtime"
	clientVersion = "v1"
)

// ServerConfig describes one remote MCP server reachable over Streamable HTTP.
// Headers are applied verbatim to every request (typically a resolved auth
// header such as "Authorization: Bearer <token>"); secret decryption happens in
// the caller so this package never touches Phrony secrets directly.
type ServerConfig struct {
	Name    string
	URL     string
	Headers map[string]string
	// HTTPClient overrides the transport's HTTP client (tests). Optional.
	HTTPClient *http.Client
}

// Client is a lazily-connected MCP session for a single server. It performs the
// initialize handshake on first use and reuses the session (and its negotiated
// session id) for subsequent tools/call requests. A Client is safe for
// concurrent use.
type Client struct {
	cfg ServerConfig

	mu      sync.Mutex
	session *mcpsdk.ClientSession
}

// NewClient builds a Client for the given server. It does not connect; the
// initialize handshake is deferred until the first CallTool.
func NewClient(cfg ServerConfig) *Client {
	return &Client{cfg: cfg}
}

// Name returns the configured server name.
func (c *Client) Name() string {
	return c.cfg.Name
}

func (c *Client) transport() *mcpsdk.StreamableClientTransport {
	httpClient := c.cfg.HTTPClient
	if len(c.cfg.Headers) > 0 {
		base := http.DefaultTransport
		if httpClient != nil && httpClient.Transport != nil {
			base = httpClient.Transport
		}
		rt := &headerRoundTripper{headers: c.cfg.Headers, base: base}
		if httpClient == nil {
			httpClient = &http.Client{Transport: rt}
		} else {
			clone := *httpClient
			clone.Transport = rt
			httpClient = &clone
		}
	}
	return &mcpsdk.StreamableClientTransport{
		Endpoint:   c.cfg.URL,
		HTTPClient: httpClient,
		// Public streamable HTTP MCP endpoints (e.g. DeepWiki) often do not expose a
		// standalone GET SSE stream; waiting on that GET can exceed tool-dispatch
		// deadlines. POST-only mode is sufficient for tools/call.
		DisableStandaloneSSE: true,
	}
}

// connectLocked establishes the MCP session if not already connected. The
// caller must hold c.mu.
func (c *Client) connectLocked(ctx context.Context) (*mcpsdk.ClientSession, error) {
	if c.session != nil {
		return c.session, nil
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: clientName, Version: clientVersion}, nil)
	session, err := client.Connect(ctx, c.transport(), nil)
	if err != nil {
		return nil, fmt.Errorf("connect mcp server %q: %w", c.cfg.Name, err)
	}
	c.session = session
	return session, nil
}

// CallTool invokes a remote MCP tool and returns the raw MCP result. A failed
// call drops the session so the next call re-runs the initialize handshake.
func (c *Client) CallTool(ctx context.Context, tool string, args json.RawMessage) (*mcpsdk.CallToolResult, error) {
	c.mu.Lock()
	session, err := c.connectLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	params := &mcpsdk.CallToolParams{Name: tool}
	if len(args) > 0 {
		params.Arguments = args
	}
	res, err := session.CallTool(ctx, params)
	if err != nil {
		c.reset(session)
		return nil, err
	}
	return res, nil
}

// reset drops the session if it is still the active one, forcing a reconnect.
func (c *Client) reset(stale *mcpsdk.ClientSession) {
	c.mu.Lock()
	if c.session == stale {
		c.session = nil
	}
	c.mu.Unlock()
	if stale != nil {
		_ = stale.Close()
	}
}

// Close terminates the underlying session, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	session := c.session
	c.session = nil
	c.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}

// headerRoundTripper injects static headers (e.g. auth) into every request.
type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		clone.Header.Set(k, v)
	}
	return h.base.RoundTrip(clone)
}
