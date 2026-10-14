// Package provider implements bounded, non-retrying translation protocols.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cvtranslate/internal/domain"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (failure *Error) Error() string     { return failure.Message }
func failure(code, message string) error { return &Error{Code: code, Message: message} }

type openAIAdapter struct{}

func (openAIAdapter) Capabilities() Capabilities {
	return Capabilities{Type: "openai-compatible", Name: "OpenAI compatible", DefaultModel: domain.DefaultOpenAIModel, SupportsPrompts: true, SupportsAPIKey: true, ConfigurableEndpoint: true, RequiresModel: true}
}

func (openAIAdapter) ValidateConfig(config domain.ProviderConfig) error {
	if strings.TrimSpace(config.Name) == "" || len(config.Name) > 200 || strings.TrimSpace(config.Model) == "" || len(config.Model) > 200 {
		return failure("invalid_provider", "Provider name and model are required and must be at most 200 bytes.")
	}
	if config.TimeoutSeconds < 1 || config.TimeoutSeconds > 300 {
		return failure("invalid_provider", "Timeout must be between 1 and 300 seconds.")
	}
	endpoint, err := url.Parse(config.BaseURL)
	if err != nil || len(config.BaseURL) > 2048 || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || strings.Contains(config.BaseURL, "#") || endpoint.Opaque != "" {
		return failure("invalid_endpoint", "Use an HTTP(S) base URL without credentials, query or fragment.")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return failure("invalid_endpoint", "Use an HTTP(S) base URL.")
	}
	return nil
}

func (openAIAdapter) Translate(ctx context.Context, config domain.ProviderConfig, apiKey string, input TranslationRequest) (TranslationResult, error) {
	if strings.ContainsAny(apiKey, "\r\n") {
		return TranslationResult{}, failure("invalid_credentials", "Credential contains invalid characters.")
	}
	if len(input.SystemPrompt)+len(input.UserPrompt) > 512*1024 || len(apiKey) > 16*1024 {
		return TranslationResult{}, failure("invalid_request", "Translation request exceeded the size limit.")
	}
	requestBody := struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
		Stream   bool                `json:"stream"`
	}{Model: config.Model, Messages: []map[string]string{{"role": "system", "content": input.SystemPrompt}, {"role": "user", "content": input.UserPrompt}}, Stream: false}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return TranslationResult{}, failure("invalid_request", "Could not encode translation request.")
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, strings.TrimRight(config.BaseURL, "/")+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return TranslationResult{}, failure("invalid_endpoint", "Invalid provider endpoint.")
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return TranslationResult{}, failure("timeout", "Provider request timed out.")
		}
		if ctx.Err() != nil {
			return TranslationResult{}, failure("cancelled", "Translation was cancelled.")
		}
		return TranslationResult{}, failure("network_error", "Could not reach the provider. Check endpoint and connection.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case 401, 403:
			return TranslationResult{}, failure("unauthorized", "Provider rejected the credentials.")
		case 429:
			return TranslationResult{}, failure("rate_limited", "Provider rate limit reached. Try again later.")
		default:
			return TranslationResult{}, failure("upstream_error", "Provider returned an unsuccessful HTTP response.")
		}
	}
	const maxResponseBytes = 2 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return TranslationResult{}, failure("timeout", "Provider request timed out.")
		}
		if ctx.Err() != nil {
			return TranslationResult{}, failure("cancelled", "Translation was cancelled.")
		}
		return TranslationResult{}, failure("invalid_response", "Could not read provider response.")
	}
	if len(body) > maxResponseBytes {
		return TranslationResult{}, failure("invalid_response", "Provider response exceeded the size limit.")
	}
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role         string          `json:"role"`
				Content      *string         `json:"content"`
				Refusal      json.RawMessage `json:"refusal"`
				ToolCalls    json.RawMessage `json:"tool_calls"`
				FunctionCall json.RawMessage `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &result) != nil || len(result.Choices) != 1 {
		return TranslationResult{}, failure("invalid_response", "Provider returned a malformed response.")
	}
	choice := result.Choices[0]
	message := choice.Message
	present := func(value json.RawMessage) bool {
		return len(value) > 0 && string(value) != "null" && string(value) != "[]" && string(value) != `""`
	}
	if choice.FinishReason != "stop" || message.Role != "assistant" || message.Content == nil || strings.TrimSpace(*message.Content) == "" || present(message.Refusal) || present(message.ToolCalls) || present(message.FunctionCall) {
		return TranslationResult{}, failure("incomplete_response", "Provider did not return a complete text translation.")
	}
	translationResult := TranslationResult{Text: *message.Content}
	if present(result.Usage) {
		var reportedUsage struct {
			PromptTokens     *int64 `json:"prompt_tokens"`
			CompletionTokens *int64 `json:"completion_tokens"`
			TotalTokens      *int64 `json:"total_tokens"`
		}
		if json.Unmarshal(result.Usage, &reportedUsage) != nil ||
			reportedUsage.PromptTokens == nil ||
			reportedUsage.CompletionTokens == nil ||
			reportedUsage.TotalTokens == nil {
			return translationResult, nil
		}
		usage := domain.TokenUsage{
			PromptTokens:     *reportedUsage.PromptTokens,
			CompletionTokens: *reportedUsage.CompletionTokens,
			TotalTokens:      *reportedUsage.TotalTokens,
		}
		const maximumReportedTokens = int64(1_000_000_000_000)
		validUsage := usage.PromptTokens >= 0 && usage.PromptTokens <= maximumReportedTokens &&
			usage.CompletionTokens >= 0 && usage.CompletionTokens <= maximumReportedTokens &&
			usage.TotalTokens >= usage.PromptTokens+usage.CompletionTokens && usage.TotalTokens <= maximumReportedTokens
		if validUsage {
			translationResult.TokenUsage = &usage
		}
	}
	return translationResult, nil
}
