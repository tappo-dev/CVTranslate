// Package translation owns the shared, non-replaying translation lifecycle.
package translation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	"cvtranslate/internal/provider"
)

type Store interface {
	GetSettings(context.Context) (domain.Settings, error)
	GetProvider(context.Context, string) (domain.ProviderConfig, error)
	GetPrompt(context.Context, string) (domain.Prompt, error)
	SaveTask(context.Context, domain.Task) error
	GetTask(context.Context, string) (domain.Task, error)
	FindReusableTask(context.Context, domain.Task) (*domain.Task, error)
}
type Secrets interface{ Get(string) (string, error) }
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (failure *Error) Error() string     { return failure.Message }
func failure(code, message string) error { return &Error{Code: code, Message: message} }

type job struct {
	task         domain.Task
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	apiKey       string
	request      provider.TranslationRequest
	persistError error
}

type Service struct {
	store      Store
	secrets    Secrets
	ctx        context.Context
	cancel     context.CancelFunc
	mutex      sync.Mutex
	jobs       map[string]*job
	queue      chan *job
	workers    sync.WaitGroup
	closed     bool
	closeOnce  sync.Once
	closeError error
	translate  func(context.Context, domain.ProviderConfig, string, provider.TranslationRequest) (provider.TranslationResult, error)
}

func New(store Store, secrets Secrets) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{store: store, secrets: secrets, ctx: ctx, cancel: cancel, jobs: make(map[string]*job), queue: make(chan *job, 32), translate: provider.TranslateWithUsage}
	for worker := 0; worker < 4; worker++ {
		service.workers.Add(1)
		go service.work()
	}
	return service
}

func validID(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > 128 {
		return false
	}
	for _, character := range identifier {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func resolveTranslationLanguages(text, sourceLanguage, targetLanguage string) (string, string, error) {
	if _, valid := domain.LanguageName(sourceLanguage); !valid {
		return "", "", failure("invalid_input", "Unknown source language.")
	}
	if _, valid := domain.LanguageName(targetLanguage); !valid {
		return "", "", failure("invalid_input", "Choose a valid target language.")
	}
	if targetLanguage != "auto" {
		return sourceLanguage, targetLanguage, nil
	}
	if sourceLanguage == "auto" {
		sourceLanguage = domain.DetectSupportedLanguage(text)
	}
	resolvedTargetLanguage, valid := domain.OppositeLanguage(sourceLanguage)
	if !valid {
		return "", "", failure("invalid_input", "Automatic target selection requires English or Simplified Chinese.")
	}
	return sourceLanguage, resolvedTargetLanguage, nil
}

func (service *Service) Start(input domain.TranslationInput) (domain.Task, error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.closed {
		return domain.Task{}, failure("service_closed", "Translation service is shutting down.")
	}
	if !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > domain.MaxTextRunes {
		return domain.Task{}, failure("invalid_input", "Text is empty or exceeds the supported limit.")
	}
	settings, err := service.store.GetSettings(service.ctx)
	if err != nil {
		return domain.Task{}, failure("storage_error", "Could not read translation settings.")
	}
	if input.ProviderID == "" {
		input.ProviderID = settings.DefaultProviderID
	}
	if input.PromptID == "" {
		input.PromptID = settings.DefaultPromptID
	}
	if input.SourceLanguage == "" {
		input.SourceLanguage = settings.SourceLanguage
	}
	if input.TargetLanguage == "" {
		input.TargetLanguage = settings.TargetLanguage
	}
	if input.Entry == "" {
		input.Entry = "web"
	}
	if !validID(input.ProviderID) {
		return domain.Task{}, failure("invalid_input", "Select a valid provider.")
	}
	if input.Entry != "web" && input.Entry != "clipboard-ui" && input.Entry != "clipboard-paste" {
		return domain.Task{}, failure("invalid_input", "Unsupported translation entry point.")
	}
	configuration, err := service.store.GetProvider(service.ctx, input.ProviderID)
	if err != nil {
		return domain.Task{}, failure("invalid_provider", "Selected provider is unavailable.")
	}
	if !configuration.Enabled {
		return domain.Task{}, failure("invalid_provider", "Selected provider is disabled.")
	}
	if err = provider.ValidateConfig(configuration); err != nil {
		return domain.Task{}, err
	}
	input.SourceLanguage, input.TargetLanguage, err = resolveTranslationLanguages(input.Text, input.SourceLanguage, input.TargetLanguage)
	if err != nil {
		return domain.Task{}, err
	}
	providerRequest := provider.TranslationRequest{Text: input.Text, SourceLanguage: input.SourceLanguage, TargetLanguage: input.TargetLanguage}
	var template domain.Prompt
	capabilities, _ := provider.CapabilitiesFor(configuration.Type)
	if capabilities.SupportsPrompts {
		if !validID(input.PromptID) {
			return domain.Task{}, failure("invalid_input", "Select a valid prompt.")
		}
		template, err = service.store.GetPrompt(service.ctx, input.PromptID)
		if err != nil {
			return domain.Task{}, failure("invalid_prompt", "Selected prompt is unavailable.")
		}
		providerRequest.SystemPrompt, providerRequest.UserPrompt, err = prompt.Render(template, input.Text, input.SourceLanguage, input.TargetLanguage)
		if err != nil {
			return domain.Task{}, failure("invalid_input", "Check prompt variables, text limits and selected languages.")
		}
	}
	configuration.HasAPIKey = false
	now := time.Now().UTC()
	task := domain.Task{ID: domain.NewID(), Text: input.Text, SourceLanguage: input.SourceLanguage, TargetLanguage: input.TargetLanguage, Entry: input.Entry, Status: "queued", ProviderSnapshot: configuration, PromptSnapshot: template, CreatedAt: now, UpdatedAt: now, PasteStatus: "not_requested"}
	reusable, err := service.store.FindReusableTask(service.ctx, task)
	if err != nil {
		// An unreadable history must not silently become another billable request.
		return domain.Task{}, failure("storage_error", "Could not check previous translations.")
	}
	if reusable != nil {
		// Keep this invocation's entry and paste outcome independent of the original.
		task.Status = "succeeded"
		task.Translation = reusable.Translation
		task.ReusedFromTaskID = reusable.ID
		task.UpdatedAt = time.Now().UTC()
		task.DurationMS = task.UpdatedAt.Sub(task.CreatedAt).Milliseconds()
		if err := service.store.SaveTask(service.ctx, task); err != nil {
			return domain.Task{}, failure("storage_error", "Could not save reused translation.")
		}
		return task, nil
	}
	if len(service.jobs) >= 32 {
		return domain.Task{}, failure("queue_full", "Too many translations are pending. Try again later.")
	}
	var apiKey string
	if capabilities.SupportsAPIKey {
		if service.secrets == nil {
			return domain.Task{}, failure("credential_error", "Credential service is unavailable.")
		}
		apiKey, err = service.secrets.Get(configuration.ID)
		if err != nil {
			return domain.Task{}, failure("credential_error", "Could not read provider credentials.")
		}
	}
	if err := service.store.SaveTask(service.ctx, task); err != nil {
		return domain.Task{}, failure("storage_error", "Could not save translation task.")
	}
	ctx, cancel := context.WithCancel(service.ctx)
	pending := &job{task: task, ctx: ctx, cancel: cancel, done: make(chan struct{}), apiKey: apiKey, request: providerRequest}
	service.jobs[task.ID] = pending
	service.queue <- pending
	return task, nil
}

func (service *Service) persist(pending *job) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.store.SaveTask(ctx, pending.task); err != nil {
		pending.persistError = failure("storage_error", "Could not persist translation state.")
		return pending.persistError
	}
	pending.persistError = nil
	return nil
}

func (service *Service) work() {
	defer service.workers.Done()
	for pending := range service.queue {
		service.mutex.Lock()
		if domain.IsTerminal(pending.task.Status) {
			service.finish(pending)
			service.mutex.Unlock()
			continue
		}
		pending.task.Status = "running"
		pending.task.UpdatedAt = time.Now().UTC()
		if err := service.persist(pending); err != nil {
			service.finish(pending)
			service.mutex.Unlock()
			continue
		}
		service.mutex.Unlock()
		translationResult, translateError := service.translate(pending.ctx, pending.task.ProviderSnapshot, pending.apiKey, pending.request)
		service.mutex.Lock()
		// Cancellation and completion share this lock; a late response cannot win.
		if !domain.IsTerminal(pending.task.Status) {
			pending.task.Status = "succeeded"
			pending.task.Translation = translationResult.Text
			pending.task.TokenUsage = translationResult.TokenUsage
			if translateError != nil {
				pending.task.Status = "failed"
				pending.task.Translation = ""
				pending.task.TokenUsage = nil
				pending.task.ErrorCode = "provider_error"
				pending.task.ErrorMessage = "Provider could not complete the translation."
				var upstream *provider.Error
				if errors.As(translateError, &upstream) {
					pending.task.ErrorCode = upstream.Code
					pending.task.ErrorMessage = upstream.Message
				}
			}
			pending.task.UpdatedAt = time.Now().UTC()
			pending.task.DurationMS = pending.task.UpdatedAt.Sub(pending.task.CreatedAt).Milliseconds()
			service.persist(pending)
		}
		service.finish(pending)
		service.mutex.Unlock()
	}
}

// finish is called once by the owning worker while holding the service mutex.
func (service *Service) finish(pending *job) {
	pending.cancel()
	pending.apiKey = ""
	pending.request = provider.TranslationRequest{}
	if pending.persistError != nil {
		service.closeError = pending.persistError
	}
	delete(service.jobs, pending.task.ID)
	close(pending.done)
}

func (service *Service) Cancel(identifier string) (domain.Task, error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if !validID(identifier) {
		return domain.Task{}, failure("invalid_input", "Invalid task identifier.")
	}
	pending, exists := service.jobs[identifier]
	if !exists {
		return service.store.GetTask(context.Background(), identifier)
	}
	if domain.IsTerminal(pending.task.Status) {
		return pending.task, pending.persistError
	}
	pending.cancel()
	pending.task.Status = "cancelled"
	pending.task.ErrorCode = "cancelled"
	pending.task.ErrorMessage = "Translation was cancelled."
	pending.task.UpdatedAt = time.Now().UTC()
	pending.task.DurationMS = pending.task.UpdatedAt.Sub(pending.task.CreatedAt).Milliseconds()
	err := service.persist(pending)
	return pending.task, err
}

func (service *Service) Wait(ctx context.Context, identifier string) (domain.Task, error) {
	if !validID(identifier) {
		return domain.Task{}, failure("invalid_input", "Invalid task identifier.")
	}
	service.mutex.Lock()
	pending := service.jobs[identifier]
	service.mutex.Unlock()
	if pending != nil {
		select {
		case <-ctx.Done():
			return domain.Task{}, ctx.Err()
		case <-pending.done:
		}
		if pending.persistError != nil {
			return domain.Task{}, pending.persistError
		}
	}
	task, err := service.store.GetTask(ctx, identifier)
	if err != nil {
		return domain.Task{}, err
	}
	if !domain.IsTerminal(task.Status) {
		return domain.Task{}, failure("storage_error", "Task outcome could not be persisted.")
	}
	return task, nil
}

func (service *Service) Close() error {
	service.closeOnce.Do(func() {
		service.mutex.Lock()
		service.closed = true
		service.cancel()
		for _, pending := range service.jobs {
			if !domain.IsTerminal(pending.task.Status) {
				pending.task.Status = "cancelled"
				pending.task.ErrorCode = "cancelled"
				pending.task.ErrorMessage = "Application shut down before translation completed."
				pending.task.UpdatedAt = time.Now().UTC()
				pending.task.DurationMS = pending.task.UpdatedAt.Sub(pending.task.CreatedAt).Milliseconds()
				if err := service.persist(pending); err != nil {
					service.closeError = err
				}
			}
		}
		close(service.queue)
		service.mutex.Unlock()
		service.workers.Wait()
	})
	return service.closeError
}
