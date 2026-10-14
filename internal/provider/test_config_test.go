package provider

import "cvtranslate/internal/domain"

func testConfig(endpoint string) domain.ProviderConfig {
	return domain.ProviderConfig{ID: "provider", Name: "Mock", Type: "openai-compatible", BaseURL: endpoint, Model: "test-model", Enabled: true, TimeoutSeconds: 2}
}
