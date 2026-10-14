package translation

import (
	"context"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	"cvtranslate/internal/provider"
)

func TestPersonaSnapshotAcrossTranslationEntries(test *testing.T) {
	for _, entry := range []string{"web", "clipboard-ui", "clipboard-paste"} {
		test.Run(entry, func(test *testing.T) {
			service, store := setupService(test)
			requestContext := context.Background()
			template := prompt.BuiltIn()
			template.ID = "persona-prompt"
			template.BuiltIn = false
			template.Persona = "Optimistic young person; soften blame without losing the request."
			if err := store.SavePrompt(requestContext, template); err != nil {
				test.Fatal(err)
			}
			settings, err := store.GetSettings(requestContext)
			if err != nil {
				test.Fatal(err)
			}
			settings.DefaultPromptID = template.ID
			if err := store.SaveSettings(requestContext, settings); err != nil {
				test.Fatal(err)
			}
			release := make(chan struct{})
			requests := make(chan provider.TranslationRequest, 1)
			service.translate = func(ctx context.Context, _ domain.ProviderConfig, _ string, request provider.TranslationRequest) (provider.TranslationResult, error) {
				select {
				case <-release:
				case <-ctx.Done():
					return provider.TranslationResult{}, ctx.Err()
				}
				requests <- request
				return provider.TranslationResult{Text: "translated"}, nil
			}
			task, err := service.Start(domain.TranslationInput{Text: "Please fix this bug.", Entry: entry})
			if err != nil {
				test.Fatal(err)
			}
			changed := template
			changed.Persona = "Different persona for future tasks."
			changed.Revision++
			if err := store.SavePrompt(requestContext, changed); err != nil {
				test.Fatal(err)
			}
			close(release)
			waitContext, cancel := context.WithTimeout(requestContext, 3*time.Second)
			defer cancel()
			result, err := service.Wait(waitContext, task.ID)
			if err != nil || result.Status != "succeeded" || result.PromptSnapshot != template {
				test.Fatalf("persona snapshot changed: %+v %v", result, err)
			}
			expectedSystem, expectedUser, err := prompt.Render(template, task.Text, task.SourceLanguage, task.TargetLanguage)
			if err != nil {
				test.Fatal(err)
			}
			request := <-requests
			if request.SystemPrompt != expectedSystem || request.UserPrompt != expectedUser || request.Text != task.Text {
				test.Fatal("translation did not send the snapshotted persona composition")
			}
		})
	}
}
