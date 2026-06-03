//go:build deepwiki

package mcp

import (
	"context"
	"testing"
	"time"
)

// Exercises DeepWiki over the same 10s dispatch-queue budget as production.
func TestDeepWikiConnectWithDispatchQueueTimeout(t *testing.T) {
	client := NewClient(ServerConfig{
		Name: "deepwiki",
		URL:  "https://mcp.deepwiki.com/mcp",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := client.CallTool(ctx, "read_wiki_structure", []byte(`{"repoName":"openai/openai-go"}`))
	t.Logf("elapsed=%s err=%v", time.Since(start), err)
	if err != nil {
		t.Fatal(err)
	}
}
