package provider

import (
	"context"
	"sort"

	"cvtranslate/internal/domain"
)

type Capabilities struct {
	Type                 string `json:"type"`
	Name                 string `json:"name"`
	DefaultModel         string `json:"defaultModel"`
	SupportsPrompts      bool   `json:"supportsPrompts"`
	SupportsAPIKey       bool   `json:"supportsApiKey"`
	ConfigurableEndpoint bool   `json:"configurableEndpoint"`
	RequiresModel        bool   `json:"requiresModel"`
}

// TranslationRequest keeps the source separate from rendered LLM messages so a
// non-LLM adapter can translate text without reverse-parsing a prompt template.
type TranslationRequest struct {
	Text           string
	SourceLanguage string
	TargetLanguage string
	SystemPrompt   string
	UserPrompt     string
}

type TranslationResult struct {
	Text       string
	TokenUsage *domain.TokenUsage
}

type Adapter interface {
	Capabilities() Capabilities
	ValidateConfig(domain.ProviderConfig) error
	Translate(context.Context, domain.ProviderConfig, string, TranslationRequest) (TranslationResult, error)
}

// Registration is fixed at compile time. There is no runtime plugin loader or
// mutable exported registry that could replace an adapter during an active job.
func compiledAdapters() map[string]Adapter {
	return map[string]Adapter{"openai-compatible": openAIAdapter{}, "microsoft-translate": microsoftAdapter{}}
}

func Registry() []Capabilities {
	result := make([]Capabilities, 0, len(compiledAdapters()))
	for _, adapter := range compiledAdapters() {
		result = append(result, adapter.Capabilities())
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Type < result[right].Type })
	return result
}

func CapabilitiesFor(providerType string) (Capabilities, bool) {
	adapter, exists := compiledAdapters()[providerType]
	if !exists {
		return Capabilities{}, false
	}
	return adapter.Capabilities(), true
}

func ValidateConfig(configuration domain.ProviderConfig) error {
	adapter, exists := compiledAdapters()[configuration.Type]
	if !exists {
		return failure("invalid_provider", "Unsupported provider type.")
	}
	return adapter.ValidateConfig(configuration)
}

func TranslateWithUsage(ctx context.Context, configuration domain.ProviderConfig, apiKey string, request TranslationRequest) (TranslationResult, error) {
	adapter, exists := compiledAdapters()[configuration.Type]
	if !exists {
		return TranslationResult{}, failure("invalid_provider", "Unsupported provider type.")
	}
	if err := adapter.ValidateConfig(configuration); err != nil {
		return TranslationResult{}, err
	}
	return adapter.Translate(ctx, configuration, apiKey, request)
}

func Translate(ctx context.Context, configuration domain.ProviderConfig, apiKey string, request TranslationRequest) (string, error) {
	result, err := TranslateWithUsage(ctx, configuration, apiKey, request)
	return result.Text, err
}
