package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"cvtranslate/internal/domain"
)

const microsoftTranslationEndpoint = "https://edge.microsoft.com/translate/translatetext"

type microsoftAdapter struct{}

func (microsoftAdapter) Capabilities() Capabilities {
	return Capabilities{Type: "microsoft-translate", Name: "Microsoft Translator"}
}

func (microsoftAdapter) ValidateConfig(configuration domain.ProviderConfig) error {
	if strings.TrimSpace(configuration.Name) == "" || len(configuration.Name) > 200 {
		return failure("invalid_provider", "Provider name is required and must be at most 200 bytes.")
	}
	if configuration.BaseURL != "" || configuration.Model != "" {
		return failure("invalid_provider", "Microsoft Translator uses a fixed endpoint and does not accept a model.")
	}
	if configuration.TimeoutSeconds < 1 || configuration.TimeoutSeconds > 300 {
		return failure("invalid_provider", "Timeout must be between 1 and 300 seconds.")
	}
	return nil
}

func (microsoftAdapter) Translate(ctx context.Context, configuration domain.ProviderConfig, apiKey string, input TranslationRequest) (TranslationResult, error) {
	if apiKey != "" {
		return TranslationResult{}, failure("invalid_credentials", "Microsoft Translator does not accept an API key.")
	}
	text, err := translateMicrosoft(ctx, configuration, input, &http.Client{}, microsoftTranslationEndpoint)
	return TranslationResult{Text: text}, err
}

func translateMicrosoft(ctx context.Context, configuration domain.ProviderConfig, input TranslationRequest, client *http.Client, endpoint string) (string, error) {
	if err := (microsoftAdapter{}).ValidateConfig(configuration); err != nil {
		return "", err
	}
	if !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > domain.MaxTextRunes {
		return "", failure("invalid_request", "Text is empty or exceeds the supported limit.")
	}
	languages := map[string]string{"auto": "", "en": "en", "zh-CN": "zh-Hans"}
	source, sourceExists := languages[input.SourceLanguage]
	target, targetExists := languages[input.TargetLanguage]
	if !sourceExists || !targetExists || target == "" {
		return "", failure("invalid_request", "Choose supported source and target languages.")
	}
	encoded, err := json.Marshal([]string{html.EscapeString(input.Text)})
	if err != nil {
		return "", failure("invalid_request", "Could not encode translation request.")
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Duration(configuration.TimeoutSeconds)*time.Second)
	defer cancel()
	query := url.Values{"from": {source}, "to": {target}, "isEnterpriseClient": {"false"}}
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint+"?"+query.Encode(), bytes.NewReader(encoded))
	if err != nil {
		return "", failure("invalid_endpoint", "Invalid provider endpoint.")
	}
	request.Header.Set("Content-Type", "application/json")
	// Copy the injected client so redirect safety cannot depend on the caller.
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	requestFailure := func(code, message string) error {
		if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return failure("timeout", "Provider request timed out.")
		}
		if ctx.Err() != nil {
			return failure("cancelled", "Translation was cancelled.")
		}
		return failure(code, message)
	}
	response, err := boundedClient.Do(request)
	if err != nil {
		return "", requestFailure("network_error", "Could not reach Microsoft Translator. Check your connection.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "", failure("unauthorized", "Microsoft Translator rejected the request.")
		case http.StatusTooManyRequests:
			return "", failure("rate_limited", "Microsoft Translator rate limit reached. Try again later.")
		default:
			return "", failure("upstream_error", "Microsoft Translator returned an unsuccessful HTTP response.")
		}
	}
	const maxResponseBytes = 2 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return "", requestFailure("invalid_response", "Could not read provider response.")
	}
	if len(body) > maxResponseBytes || !utf8.Valid(body) {
		return "", failure("invalid_response", "Provider response is invalid or exceeds the size limit.")
	}
	var result []struct {
		Translations []struct {
			Text string `json:"text"`
			To   string `json:"to"`
		} `json:"translations"`
	}
	if json.Unmarshal(body, &result) != nil || len(result) != 1 || len(result[0].Translations) != 1 {
		return "", failure("invalid_response", "Provider returned a malformed response.")
	}
	translated := result[0].Translations[0]
	text := html.UnescapeString(translated.Text)
	if translated.To != target || strings.TrimSpace(text) == "" {
		return "", failure("incomplete_response", "Provider did not return a complete text translation.")
	}
	return text, nil
}
