package translation

import (
	"context"
	"errors"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	"cvtranslate/internal/provider"
)

type unavailableSecrets struct{}

func (unavailableSecrets) Get(string) (string, error) { return "", errors.New("keyring unavailable") }

func TestMicrosoftSkipsCredentialsAndPrompts(test *testing.T) {
	for _, secrets := range []Secrets{nil, unavailableSecrets{}} {
		service, store := setupService(test)
		service.secrets = secrets
		settings := domain.DefaultSettings()
		settings.DefaultPromptID = "missing-prompt"
		if err := store.SaveSettings(context.Background(), settings); err != nil {
			test.Fatal(err)
		}
		service.translate = func(_ context.Context, configuration domain.ProviderConfig, key string, request provider.TranslationRequest) (provider.TranslationResult, error) {
			if configuration.Type != "microsoft-translate" || key != "" || request.SystemPrompt != "" || request.UserPrompt != "" || request.Text != "raw text" {
				test.Error("pure translation contract violated")
			}
			return provider.TranslationResult{Text: "translated"}, nil
		}
		task, err := service.Start(domain.TranslationInput{Text: "raw text"})
		if err != nil {
			test.Fatal(err)
		}
		waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := service.Wait(waitContext, task.ID)
		cancel()
		if err != nil || result.Status != "succeeded" || result.PromptSnapshot != (domain.Prompt{}) || result.ProviderSnapshot.HasAPIKey {
			test.Fatalf("Microsoft job: %+v %v", result, err)
		}
		_, err = service.Start(domain.TranslationInput{Text: "raw text", ProviderID: "mock", PromptID: "builtin-general"})
		var safe *Error
		if !errors.As(err, &safe) || safe.Code != "credential_error" {
			test.Fatalf("OpenAI did not fail closed: %v", err)
		}
	}
}

func TestMicrosoftIgnoresConfiguredPersona(test *testing.T) {
	service, store := setupService(test)
	template := prompt.BuiltIn()
	template.ID = "persona-prompt"
	template.BuiltIn = false
	template.Persona = "Use a cheerful and friendly tone."
	if err := store.SavePrompt(context.Background(), template); err != nil {
		test.Fatal(err)
	}
	service.translate = func(_ context.Context, _ domain.ProviderConfig, _ string, request provider.TranslationRequest) (provider.TranslationResult, error) {
		if request.SystemPrompt != "" || request.UserPrompt != "" || request.Text != "raw text" {
			test.Error("persona instructions reached a non-prompt provider")
		}
		return provider.TranslationResult{Text: "translated"}, nil
	}
	task, err := service.Start(domain.TranslationInput{Text: "raw text", ProviderID: domain.BuiltInMicrosoftProviderID, PromptID: template.ID})
	if err != nil {
		test.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.Wait(waitContext, task.ID)
	if err != nil || result.Status != "succeeded" || result.PromptSnapshot != (domain.Prompt{}) {
		test.Fatalf("Microsoft task retained an unused persona: %+v %v", result, err)
	}
}
