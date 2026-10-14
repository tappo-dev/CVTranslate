package translation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/provider"
)

func completeTranslation(test *testing.T, service *Service, input domain.TranslationInput) domain.Task {
	test.Helper()
	task, err := service.Start(input)
	if err != nil {
		test.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	completed, err := service.Wait(waitContext, task.ID)
	if err != nil || completed.Status != "succeeded" {
		test.Fatalf("translation did not complete: status=%s error=%v", completed.Status, err)
	}
	return completed
}

func TestReuseSkipsProviderAndCredentialsWithoutDuplicatingUsage(test *testing.T) {
	service, store := setupService(test)
	var calls atomic.Int32
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		calls.Add(1)
		return provider.TranslationResult{
			Text:       "translated",
			TokenUsage: &domain.TokenUsage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100},
		}, nil
	}
	input := domain.TranslationInput{Text: "previous input"}
	original := completeTranslation(test, service, input)
	requestContext := context.Background()
	if err := store.UpdatePasteStatus(requestContext, original.ID, "skipped"); err != nil {
		test.Fatal(err)
	}
	if err := service.Close(); err != nil {
		test.Fatal(err)
	}
	// A new service must reuse persisted history, not just process-local memory.
	restarted := New(store, unavailableSecrets{})
	restarted.translate = service.translate
	defer restarted.Close()
	for _, entry := range []string{"web", "clipboard-ui", "clipboard-paste"} {
		input.Entry = entry
		input.SourceLanguage = original.SourceLanguage
		input.TargetLanguage = original.TargetLanguage
		reused, err := restarted.Start(input)
		if err != nil {
			test.Fatal(err)
		}
		if reused.Status != "succeeded" || reused.Translation != original.Translation || reused.ReusedFromTaskID == "" {
			test.Fatal("successful history was not returned immediately")
		}
		if reused.ID == original.ID || reused.Entry != entry || reused.PasteStatus != "not_requested" || reused.TokenUsage != nil {
			test.Fatal("reuse inherited another invocation's identity, paste outcome or usage")
		}
		if _, err := restarted.Wait(requestContext, reused.ID); err != nil {
			test.Fatal(err)
		}
		if err := store.UpdatePasteStatus(requestContext, reused.ID, "dispatched"); err != nil {
			test.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		test.Fatalf("provider called %d times instead of once", calls.Load())
	}
	preserved, err := store.GetTask(requestContext, original.ID)
	if err != nil || preserved.PasteStatus != "skipped" || preserved.TokenUsage == nil {
		test.Fatal("original history was modified by reuse")
	}
	report, err := store.GetTokenReport(requestContext, 30, 0)
	if err != nil || report.Last30Days.RequestCount != 1 || report.Last30Days.TotalTokens != 100 {
		test.Fatalf("reuse changed token accounting: %+v, error=%v", report.Last30Days, err)
	}
}

func TestChangedModelMissesHistoryAndRestoredModelReusesIt(test *testing.T) {
	service, store := setupService(test)
	var calls atomic.Int32
	service.translate = func(_ context.Context, configuration domain.ProviderConfig, _ string, _ provider.TranslationRequest) (provider.TranslationResult, error) {
		calls.Add(1)
		return provider.TranslationResult{Text: configuration.Model}, nil
	}
	input := domain.TranslationInput{Text: "same text"}
	original := completeTranslation(test, service, input)
	configuration := original.ProviderSnapshot
	configuration.Model = "different-model"
	if err := store.SaveProvider(context.Background(), configuration); err != nil {
		test.Fatal(err)
	}
	changed := completeTranslation(test, service, input)
	if changed.Translation != "different-model" || changed.ReusedFromTaskID != "" || calls.Load() != 2 {
		test.Fatal("model change reused an incompatible result")
	}
	configuration.Model = original.ProviderSnapshot.Model
	configuration.Name = "renamed provider"
	configuration.TimeoutSeconds = 10
	if err := store.SaveProvider(context.Background(), configuration); err != nil {
		test.Fatal(err)
	}
	reused := completeTranslation(test, service, input)
	if reused.ReusedFromTaskID != original.ID || reused.Translation != original.Translation || calls.Load() != 2 {
		test.Fatal("restoring the original model did not reuse its history")
	}
	if reused.ProviderSnapshot.Name != configuration.Name {
		test.Fatal("reuse did not snapshot the current configuration")
	}
}

func TestMicrosoftReuseIgnoresUnusedPromptsAndClearingHistoryRemovesReuse(test *testing.T) {
	service, store := setupService(test)
	var calls atomic.Int32
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		calls.Add(1)
		return provider.TranslationResult{Text: "translated"}, nil
	}
	input := domain.TranslationInput{Text: "source", ProviderID: domain.BuiltInMicrosoftProviderID, PromptID: "missing-prompt"}
	original := completeTranslation(test, service, input)
	input.PromptID = "another-unused-prompt"
	reused := completeTranslation(test, service, input)
	if reused.ReusedFromTaskID != original.ID || calls.Load() != 1 {
		test.Fatal("unused prompt prevented pure-provider reuse")
	}
	if err := store.ClearTasks(context.Background()); err != nil {
		test.Fatal(err)
	}
	afterClear := completeTranslation(test, service, input)
	if afterClear.ReusedFromTaskID != "" || calls.Load() != 2 {
		test.Fatal("deleted history was still used as a cache")
	}
}

type failingReuseStore struct {
	Store
	failSave bool
}

func (store failingReuseStore) FindReusableTask(ctx context.Context, candidate domain.Task) (*domain.Task, error) {
	if !store.failSave {
		return nil, errors.New("history lookup failed")
	}
	return store.Store.FindReusableTask(ctx, candidate)
}

func (store failingReuseStore) SaveTask(context.Context, domain.Task) error {
	return errors.New("history save failed")
}

func TestReuseStorageFailureNeverFallsBackToProvider(test *testing.T) {
	service, store := setupService(test)
	var calls atomic.Int32
	service.translate = func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error) {
		calls.Add(1)
		return provider.TranslationResult{Text: "translated"}, nil
	}
	input := domain.TranslationInput{Text: "source"}
	completeTranslation(test, service, input)
	for _, failSave := range []bool{false, true} {
		service.store = failingReuseStore{Store: store, failSave: failSave}
		_, err := service.Start(input)
		var safe *Error
		if !errors.As(err, &safe) || safe.Code != "storage_error" || calls.Load() != 1 {
			test.Fatalf("history failure was not fail-closed: %v, provider calls=%d", err, calls.Load())
		}
	}
}
