package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
)

type completionErrorContext struct {
	providerID string
	baseURL    string
	model      string
}

func formatCompletionError(providerID, baseURL, model string, err error) error {
	if err == nil {
		return nil
	}
	return completionErrorContext{
		providerID: providerID,
		baseURL:    strings.TrimSpace(baseURL),
		model:      strings.TrimSpace(model),
	}.format(err)
}

func (c completionErrorContext) format(err error) error {
	var openAIErr *openai.Error
	if errors.As(err, &openAIErr) {
		return c.formatOpenAIAPIError(openAIErr)
	}

	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) {
		return c.formatAnthropicAPIError(anthropicErr)
	}

	if isLikelyWrongEndpointResponse(err) {
		return c.formatWrongEndpointError(err)
	}

	return c.formatGeneric(err)
}

func isLikelyWrongEndpointResponse(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") ||
		strings.Contains(msg, "invalid character") ||
		strings.Contains(msg, "cannot unmarshal")
}

func (c completionErrorContext) formatOpenAIAPIError(apiErr *openai.Error) error {
	endpoint := requestEndpoint(apiErr.Request)
	detail := strings.TrimSpace(apiErr.Error())
	if detail == "" {
		detail = strings.TrimSpace(apiErr.RawJSON())
	}

	var b strings.Builder
	b.WriteString("model API request failed")
	c.writeModelContext(&b)
	if endpoint != "" {
		fmt.Fprintf(&b, ": %s", endpoint)
	}
	if apiErr.StatusCode > 0 {
		fmt.Fprintf(&b, " returned HTTP %d %s", apiErr.StatusCode, http.StatusText(apiErr.StatusCode))
	}
	if detail != "" && !strings.Contains(detail, endpoint) {
		fmt.Fprintf(&b, ": %s", detail)
	}
	if hint := httpStatusHint(c.providerID, apiErr.StatusCode); hint != "" {
		fmt.Fprintf(&b, ". %s", hint)
	}
	return errors.New(b.String())
}

func (c completionErrorContext) formatAnthropicAPIError(apiErr *anthropic.Error) error {
	endpoint := requestEndpoint(apiErr.Request)
	detail := strings.TrimSpace(apiErr.Error())

	var b strings.Builder
	b.WriteString("model API request failed")
	c.writeModelContext(&b)
	if endpoint != "" {
		fmt.Fprintf(&b, ": %s", endpoint)
	}
	if apiErr.StatusCode > 0 {
		fmt.Fprintf(&b, " returned HTTP %d %s", apiErr.StatusCode, http.StatusText(apiErr.StatusCode))
	}
	if detail != "" && !strings.Contains(detail, endpoint) {
		fmt.Fprintf(&b, ": %s", detail)
	}
	if hint := httpStatusHint(c.providerID, apiErr.StatusCode); hint != "" {
		fmt.Fprintf(&b, ". %s", hint)
	}
	return errors.New(b.String())
}

func (c completionErrorContext) formatWrongEndpointError(err error) error {
	var b strings.Builder
	b.WriteString("model API returned an invalid response")
	if c.baseURL != "" {
		fmt.Fprintf(&b, " from base_url %s", c.baseURL)
	}
	b.WriteString(": the endpoint may be wrong or returned non-JSON content")
	if c.providerID == IDOpenAICompatible {
		b.WriteString(". For openai-compatible providers, spec.model.base_url must end with /v1 (for example https://openrouter.ai/api/v1 or http://localhost:11434/v1)")
	}
	fmt.Fprintf(&b, " (%v)", err)
	return errors.New(b.String())
}

func (c completionErrorContext) formatGeneric(err error) error {
	var b strings.Builder
	b.WriteString("model completion failed")
	c.writeModelContext(&b)
	fmt.Fprintf(&b, ": %v", err)
	return errors.New(b.String())
}

func (c completionErrorContext) writeModelContext(b *strings.Builder) {
	if c.model != "" {
		fmt.Fprintf(b, " (model=%s)", c.model)
	}
	if c.baseURL != "" {
		fmt.Fprintf(b, " (base_url=%s)", c.baseURL)
	}
}

func requestEndpoint(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.String()
}

func httpStatusHint(providerID string, statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if providerID == IDOpenAICompatible {
			return "Check spec.model.secret and that the referenced environment variable is set when running the session"
		}
		return "Check that the provider API key secret is configured and available to the session"
	case http.StatusNotFound:
		if providerID == IDOpenAICompatible {
			return "Check spec.model.base_url points at the provider's Chat Completions API and ends with /v1"
		}
		return "Check the configured model name and provider"
	case http.StatusTooManyRequests:
		return "The provider rate-limited this request; retry later or reduce concurrency"
	default:
		if statusCode >= 500 {
			return "The model provider returned a server error; retry later"
		}
	}
	return ""
}
