package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestFormatCompletionError_openAIWrongEndpointSSE(t *testing.T) {
	err := formatCompletionError(IDOpenAICompatible, "https://openrouter.ai", "gpt-4o", errString("unexpected end of JSON input"))
	msg := err.Error()
	for _, want := range []string{
		"invalid response",
		"base_url https://openrouter.ai",
		"spec.model.base_url must end with /v1",
		"https://openrouter.ai/api/v1",
		"unexpected end of JSON input",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want substring %q", msg, want)
		}
	}
}

func TestFormatCompletionError_openAIHTTP401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Missing Authentication header","code":401}`))
	}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(srv.URL),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: openai.ChatModel("gpt-4o"),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
	})
	for stream.Next() {
	}
	err := formatCompletionError(IDOpenAICompatible, srv.URL, "gpt-4o", stream.Err())
	if err == nil {
		t.Fatal("formatCompletionError() = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{
		"model API request failed",
		"HTTP 401",
		"base_url=" + srv.URL,
		"spec.model.secret",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want substring %q", msg, want)
		}
	}
}

func TestOpenAIProvider_Complete_wrongBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\n\n"))
	}))
	defer srv.Close()

	p := newOpenAICompatibleProvider("test-key", srv.URL)
	ch := make(chan CompletionEvent, 4)
	err := p.Complete(context.Background(), CompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, ch)
	if err == nil {
		t.Fatal("Complete() = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid response") {
		t.Fatalf("error = %q, want invalid response hint", msg)
	}
	if !strings.Contains(msg, "spec.model.base_url must end with /v1") {
		t.Fatalf("error = %q, want base_url hint", msg)
	}
}

func TestOpenAIProvider_Complete_http404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(srv.URL),
	)
	p := &openAIProvider{id: IDOpenAICompatible, client: client, baseURL: srv.URL}
	ch := make(chan CompletionEvent, 4)
	err := p.Complete(context.Background(), CompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, ch)
	if err == nil {
		t.Fatal("Complete() = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 404") {
		t.Fatalf("error = %q, want HTTP 404", msg)
	}
	if !strings.Contains(msg, "spec.model.base_url") {
		t.Fatalf("error = %q, want base_url hint", msg)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
