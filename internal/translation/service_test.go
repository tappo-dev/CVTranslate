package translation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	"cvtranslate/internal/provider"
	"cvtranslate/internal/storage"
)

type mockSecrets struct{}

func (mockSecrets) Get(string) (string, error) { return "mock-secret", nil }

func setupService(t *testing.T) (*Service, *storage.Store) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	config := domain.ProviderConfig{ID: "mock", Name: "Mock", Type: "openai-compatible", BaseURL: "https://unused.example", Model: "original", TimeoutSeconds: 1, Enabled: true}
	if err := store.SaveProvider(ctx, config); err != nil {
		t.Fatal(err)
	}
	settings := domain.DefaultSettings()
	settings.DefaultProviderID = config.ID
	if err := store.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	service := New(store, mockSecrets{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
		store.Close()
	})
	return service, store
}

func TestSnapshotsAndFastWait(t *testing.T) {
	service, store := setupService(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	service.translate = func(ctx context.Context, config domain.ProviderConfig, key string, request provider.TranslationRequest) (provider.TranslationResult, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return provider.TranslationResult{}, ctx.Err()
		}
		if config.Model != "original" || key != "mock-secret" || request.UserPrompt != "input" || request.Text != "input" || request.TargetLanguage != "zh-CN" {
			t.Error("snapshot changed")
		}
		return provider.TranslationResult{Text: "translated"}, nil
	}
	task, err := service.Start(domain.TranslationInput{Text: "input"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	config, _ := store.GetProvider(context.Background(), "mock")
	config.Model = "changed"
	if err := store.SaveProvider(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := service.Wait(ctx, task.ID)
	if err != nil || result.Status != "succeeded" || result.ProviderSnapshot.Model != "original" || result.PromptSnapshot != prompt.BuiltIn() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = service.Wait(ctx, task.ID)
	if err != nil || result.Translation != "translated" {
		t.Fatalf("fast completion wait: %v", err)
	}
}

func TestAutomaticTargetSelectsOppositeLanguage(test *testing.T) {
	service, _ := setupService(test)
	requests := make(chan provider.TranslationRequest, 3)
	service.translate = func(_ context.Context, _ domain.ProviderConfig, _ string, request provider.TranslationRequest) (provider.TranslationResult, error) {
		requests <- request
		return provider.TranslationResult{Text: "translated"}, nil
	}
	scenarios := []struct {
		name           string
		text           string
		sourceLanguage string
		expectedSource string
		expectedTarget string
	}{
		{name: "detected Chinese becomes English", text: "hello，世界", sourceLanguage: "auto", expectedSource: "zh-CN", expectedTarget: "en"},
		{name: "detected English becomes Chinese", text: "Hello, world.", sourceLanguage: "auto", expectedSource: "en", expectedTarget: "zh-CN"},
		{name: "explicit Chinese becomes English", text: "English product name", sourceLanguage: "zh-CN", expectedSource: "zh-CN", expectedTarget: "en"},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			task, err := service.Start(domain.TranslationInput{
				Text:           scenario.text,
				SourceLanguage: scenario.sourceLanguage,
				TargetLanguage: "auto",
			})
			if err != nil {
				test.Fatal(err)
			}
			if task.SourceLanguage != scenario.expectedSource || task.TargetLanguage != scenario.expectedTarget {
				test.Fatalf("task language direction = %s -> %s", task.SourceLanguage, task.TargetLanguage)
			}
			waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
			result, err := service.Wait(waitContext, task.ID)
			cancel()
			if err != nil || result.Status != "succeeded" {
				test.Fatalf("automatic target task failed: %+v %v", result, err)
			}
			request := <-requests
			if request.SourceLanguage != scenario.expectedSource || request.TargetLanguage != scenario.expectedTarget {
				test.Fatalf("provider language direction = %s -> %s", request.SourceLanguage, request.TargetLanguage)
			}
		})
	}
}

func TestSuccessfulAIUsageIsPersistedWithTask(test *testing.T) {
	service, store := setupService(test)
	expectedUsage := domain.TokenUsage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100}
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		return provider.TranslationResult{Text: "translated", TokenUsage: &expectedUsage}, nil
	}
	task, err := service.Start(domain.TranslationInput{Text: "source"})
	if err != nil {
		test.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.Wait(waitContext, task.ID)
	if err != nil || result.TokenUsage == nil || *result.TokenUsage != expectedUsage {
		test.Fatalf("task usage = %+v, error = %v", result.TokenUsage, err)
	}
	stored, err := store.GetTask(waitContext, task.ID)
	if err != nil || stored.TokenUsage == nil || *stored.TokenUsage != expectedUsage {
		test.Fatalf("persisted usage = %+v, error = %v", stored.TokenUsage, err)
	}
}

func TestRemovedSelectionEntriesAreRejected(test *testing.T) {
	service, _ := setupService(test)
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		test.Fatal("removed selection entry reached the provider")
		return provider.TranslationResult{}, nil
	}
	for _, entry := range []string{"selection-paste", "selection-ui"} {
		if _, err := service.Start(domain.TranslationInput{Text: "selected text", Entry: entry}); err == nil {
			test.Fatalf("removed entry %q was accepted", entry)
		}
	}
}

func TestCancelRejectsLateSuccess(t *testing.T) {
	service, store := setupService(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		close(entered)
		<-release
		return provider.TranslationResult{Text: "late success"}, nil
	}
	task, err := service.Start(domain.TranslationInput{Text: "input"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	result, err := service.Cancel(task.ID)
	if err != nil || result.Status != "cancelled" {
		t.Fatalf("cancel failed: %v", err)
	}
	once.Do(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err = service.Wait(ctx, task.ID)
	if err != nil || result.Status != "cancelled" || result.Translation != "" {
		t.Fatalf("cancellation overwritten: %+v %v", result, err)
	}
	stored, err := store.GetTask(ctx, task.ID)
	if err != nil || stored.Status != "cancelled" {
		t.Fatal("cancellation not persisted")
	}
}

func TestBoundedQueueAndClose(t *testing.T) {
	service, _ := setupService(t)
	service.translate = func(ctx context.Context, _ domain.ProviderConfig, _ string, _ provider.TranslationRequest) (provider.TranslationResult, error) {
		<-ctx.Done()
		return provider.TranslationResult{}, ctx.Err()
	}
	var identifiers []string
	for index := 0; index < 32; index++ {
		task, err := service.Start(domain.TranslationInput{Text: "input"})
		if err != nil {
			t.Fatal(err)
		}
		identifiers = append(identifiers, task.ID)
	}
	_, err := service.Start(domain.TranslationInput{Text: "input"})
	var safe *Error
	if !errors.As(err, &safe) || safe.Code != "queue_full" {
		t.Fatalf("queue not bounded: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	for _, identifier := range identifiers {
		result, err := service.Wait(context.Background(), identifier)
		if err != nil || result.Status != "cancelled" {
			t.Fatalf("shutdown task: %+v %v", result, err)
		}
	}
	if _, err := service.Start(domain.TranslationInput{Text: "input"}); err == nil {
		t.Fatal("start after close accepted")
	}
}
