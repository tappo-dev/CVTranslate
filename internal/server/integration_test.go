package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/storage"
	"cvtranslate/internal/translation"
)

type memoryCredentials struct {
	mutex     sync.Mutex
	values    map[string]string
	failWrite bool
}

func (credentials *memoryCredentials) Get(identifier string) (string, error) {
	credentials.mutex.Lock()
	defer credentials.mutex.Unlock()
	return credentials.values[identifier], nil
}
func (credentials *memoryCredentials) Has(identifier string) (bool, error) {
	value, err := credentials.Get(identifier)
	return value != "", err
}
func (credentials *memoryCredentials) Set(identifier, value string) error {
	credentials.mutex.Lock()
	defer credentials.mutex.Unlock()
	if credentials.failWrite {
		return errors.New("test credential failure")
	}
	credentials.values[identifier] = value
	return nil
}
func (credentials *memoryCredentials) Delete(identifier string) error {
	credentials.mutex.Lock()
	defer credentials.mutex.Unlock()
	delete(credentials.values, identifier)
	return nil
}

func newIntegrationServer(t *testing.T) *Server {
	t.Helper()
	repository, err := storage.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	credentials := &memoryCredentials{values: map[string]string{}}
	jobs := translation.New(repository, credentials)
	t.Cleanup(func() { _ = jobs.Close(); _ = repository.Close() })
	return New(repository, jobs, Options{Origin: testOrigin, Credentials: credentials})
}

func jsonText(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestProviderTaskHistoryAndSecretBoundary(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer only-in-keychain" {
			t.Error("unexpected provider routing/authentication")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"translated result"}}]}`))
	}))
	defer upstream.Close()
	application := newIntegrationServer(t)
	key := "only-in-keychain"
	configuration := providerSave{ProviderConfig: domain.ProviderConfig{Name: "Local mock", Type: "openai-compatible", BaseURL: upstream.URL + "/v1", Model: "test-model", Enabled: true, TimeoutSeconds: 10}, APIKey: &key}
	response := performRequest(application, "POST", "/api/providers", jsonText(t, configuration), testOrigin)
	if response.Code != http.StatusOK {
		t.Fatalf("provider: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), key) {
		t.Fatal("credential returned by API")
	}
	var saved domain.ProviderConfig
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.HasAPIKey {
		t.Fatal("missing credential indicator")
	}
	stored, err := application.store.GetProvider(context.Background(), saved.ID)
	if err != nil || stored.HasAPIKey {
		t.Fatal("derived credential state persisted")
	}
	input := domain.TranslationInput{Text: "original", SourceLanguage: "en", TargetLanguage: "zh-CN", ProviderID: saved.ID, PromptID: "builtin-general", Entry: "clipboard-paste"}
	response = performRequest(application, "POST", "/api/tasks", jsonText(t, input), testOrigin)
	if response.Code != http.StatusAccepted {
		t.Fatalf("task: %d %s", response.Code, response.Body.String())
	}
	var task domain.Task
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := application.jobs.Wait(waitContext, task.ID)
	if err != nil || result.Status != "succeeded" || result.Translation != "translated result" {
		t.Fatalf("result: %+v %v", result, err)
	}
	if result.Entry != "web" {
		t.Fatal("browser forged native paste task entry")
	}
	response = performRequest(application, "POST", "/api/tasks", jsonText(t, input), testOrigin)
	var reused domain.Task
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &reused) != nil {
		t.Fatal("repeat request did not return a task")
	}
	if reused.Status != "succeeded" || reused.ReusedFromTaskID != result.ID || reused.ID == result.ID || reused.Entry != "web" || upstreamCalls.Load() != 1 {
		t.Fatal("repeat HTTP request contacted the provider or lost the task boundary")
	}
	response = performRequest(application, "GET", "/api/tasks/"+reused.ID, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"reusedFromTaskId"`) {
		t.Fatal("reuse provenance was not persisted for subsequent task loads")
	}
	response = performRequest(application, "GET", "/api/history?q=original", "", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), key) || !strings.Contains(response.Body.String(), "translated result") {
		t.Fatal("history missing result or leaking credential")
	}
	configuration.ProviderConfig = saved
	configuration.BaseURL = "https://other.example/v1"
	configuration.APIKey = nil
	response = performRequest(application, "POST", "/api/providers", jsonText(t, configuration), testOrigin)
	if response.Code != http.StatusConflict {
		t.Fatal("endpoint change reused key without confirmation")
	}
}

func TestHistoryDateFilter(test *testing.T) {
	application := newIntegrationServer(test)
	for _, task := range []domain.Task{
		{ID: "outside-date", Status: "succeeded", Text: "outside", CreatedAt: time.Date(2026, 10, 13, 23, 59, 59, 0, time.UTC)},
		{ID: "inside-date", Status: "succeeded", Text: "inside", CreatedAt: time.Date(2026, 10, 14, 8, 30, 0, 0, time.UTC)},
	} {
		if err := application.store.SaveTask(context.Background(), task); err != nil {
			test.Fatal(err)
		}
	}
	createdAfter := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	createdBefore := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	response := performRequest(application, "GET", "/api/history?createdAfter="+url.QueryEscape(createdAfter)+"&createdBefore="+url.QueryEscape(createdBefore), "", "")
	if response.Code != http.StatusOK {
		test.Fatalf("date filter failed: %d %s", response.Code, response.Body.String())
	}
	var page domain.TaskPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		test.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "inside-date" {
		test.Fatalf("unexpected date-filtered history: %+v", page)
	}

	response = performRequest(application, "GET", "/api/history?createdAfter=invalid", "", "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_filter") {
		test.Fatalf("invalid date filter accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPProviderSaveAndEndpointConfirmation(test *testing.T) {
	application := newIntegrationServer(test)
	apiKey := "test-credential"
	configuration := providerSave{
		ProviderConfig: domain.ProviderConfig{
			Name: "Remote test service", Type: "openai-compatible",
			BaseURL: "https://provider.example:8080/v1", Model: "test-model",
			Enabled: true, TimeoutSeconds: 10,
		},
		APIKey: &apiKey,
	}
	response := performRequest(application, "POST", "/api/providers", jsonText(test, configuration), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("initial save failed: %d %s", response.Code, response.Body.String())
	}
	var saved domain.ProviderConfig
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		test.Fatal(err)
	}
	configuration.ProviderConfig = saved
	configuration.BaseURL = "http://provider.example:8080/v1"
	configuration.APIKey = nil
	response = performRequest(application, "POST", "/api/providers", jsonText(test, configuration), testOrigin)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "endpoint_confirmation") {
		test.Fatalf("HTTP downgrade reused a key without endpoint confirmation: %d %s", response.Code, response.Body.String())
	}
	stored, err := application.store.GetProvider(context.Background(), saved.ID)
	if err != nil || stored.BaseURL != saved.BaseURL {
		test.Fatalf("unconfirmed change modified the saved endpoint: %v", err)
	}
	configuration.ConfirmEndpointChange = true
	response = performRequest(application, "POST", "/api/providers", jsonText(test, configuration), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("confirmed HTTP endpoint rejected: %d %s", response.Code, response.Body.String())
	}
	stored, err = application.store.GetProvider(context.Background(), saved.ID)
	if err != nil || stored.BaseURL != configuration.BaseURL {
		test.Fatalf("HTTP endpoint was not persisted: %v", err)
	}
	storedKey, err := application.options.Credentials.Get(saved.ID)
	if err != nil || storedKey != apiKey || strings.Contains(response.Body.String(), apiKey) {
		test.Fatal("HTTP endpoint change failed to preserve the credential boundary")
	}
}

func TestNewOpenAIProviderUsesCodeDefaultModel(test *testing.T) {
	application := newIntegrationServer(test)
	configuration := domain.ProviderConfig{
		Name:           "Default model",
		Type:           "openai-compatible",
		BaseURL:        "https://example.test/v1",
		TimeoutSeconds: 30,
		Enabled:        true,
	}
	response := performRequest(application, "POST", "/api/providers", jsonText(test, configuration), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("provider creation failed: %d %s", response.Code, response.Body.String())
	}
	var saved domain.ProviderConfig
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		test.Fatal(err)
	}
	if saved.Model != domain.DefaultOpenAIModel {
		test.Fatalf("default model = %q", saved.Model)
	}
	stateResponse := performRequest(application, "GET", "/api/state", "", "")
	if stateResponse.Code != http.StatusOK || !strings.Contains(stateResponse.Body.String(), `"defaultModel":"gpt-6-luna"`) {
		test.Fatalf("state omitted default model: %d %s", stateResponse.Code, stateResponse.Body.String())
	}
}

func TestTokenReportEndpointValidatesQuery(test *testing.T) {
	application := newIntegrationServer(test)
	for _, requestPath := range []string{
		"/api/token-report?days=abc&timezoneOffsetMinutes=0",
		"/api/token-report?days=365&timezoneOffsetMinutes=invalid",
		"/api/token-report?days=10&timezoneOffsetMinutes=0",
	} {
		response := performRequest(application, "GET", requestPath, "", "")
		if response.Code != http.StatusBadRequest {
			test.Fatalf("invalid report query accepted: %s: %d", requestPath, response.Code)
		}
	}
	response := performRequest(application, "GET", "/api/token-report?days=30&timezoneOffsetMinutes=480", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"daily"`) {
		test.Fatalf("valid report query failed: %d %s", response.Code, response.Body.String())
	}
}

func TestSettingsRollbackAndBuiltinImmutability(t *testing.T) {
	application := newIntegrationServer(t)
	previous, err := application.store.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	application.options.ConfigureHotkeys = func(domain.Hotkeys) error { return errors.New("shortcut already registered") }
	changed := previous
	changed.Hotkeys.OpenUI = "Ctrl+Alt+T"
	changed.Speech.EdgeVoices.English = "en-US-JennyNeural"
	response := performRequest(application, "PUT", "/api/settings", jsonText(t, changed), testOrigin)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict got %d", response.Code)
	}
	actual, _ := application.store.GetSettings(context.Background())
	if actual != previous {
		t.Fatal("failed hotkey registration changed persisted settings")
	}
	builtin, err := application.store.GetPrompt(context.Background(), "builtin-general")
	if err != nil {
		t.Fatal(err)
	}
	builtin.Name = "changed"
	response = performRequest(application, "POST", "/api/prompts", jsonText(t, builtin), testOrigin)
	if response.Code != http.StatusConflict {
		t.Fatal("built-in prompt modified")
	}
	response = performRequest(application, "DELETE", "/api/prompts/builtin-general", "", testOrigin)
	if response.Code != http.StatusConflict {
		t.Fatal("built-in prompt deleted")
	}
}

func TestSettingsPersistOnlyConfigurableOpenUIShortcut(test *testing.T) {
	application := newIntegrationServer(test)
	settings, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	settings.Hotkeys = domain.Hotkeys{OpenUI: "Ctrl+Space"}
	var configured domain.Hotkeys
	application.options.ConfigureHotkeys = func(keys domain.Hotkeys) error { configured = keys; return nil }
	response := performRequest(application, "PUT", "/api/settings", jsonText(test, settings), testOrigin)
	if response.Code != http.StatusOK || configured != settings.Hotkeys {
		test.Fatalf("settings did not configure the open-WebUI binding: %d", response.Code)
	}
	var saved domain.Settings
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		test.Fatal(err)
	}
	if saved != settings {
		test.Fatal("API changed the configurable open-WebUI shortcut")
	}
	application.options.ConfigureHotkeys = func(domain.Hotkeys) error { return errors.New("shortcut conflict") }
	changed := saved
	changed.Hotkeys.OpenUI = "Ctrl+Alt+Z"
	response = performRequest(application, "PUT", "/api/settings", jsonText(test, changed), testOrigin)
	if response.Code != http.StatusConflict {
		test.Fatal("open-WebUI registration failure was hidden")
	}
	persisted, err := application.store.GetSettings(context.Background())
	if err != nil || persisted != saved {
		test.Fatal("failed shortcut registration changed persisted configuration")
	}

	legacyPayload := strings.Replace(jsonText(test, saved), `"openUi":"Ctrl+Space"`, `"openUi":"Ctrl+Space","selectionUi":"Ctrl+Alt+Y"`, 1)
	response = performRequest(application, "PUT", "/api/settings", legacyPayload, testOrigin)
	if response.Code != http.StatusBadRequest {
		test.Fatal("removed selected-text shortcut field was still accepted")
	}
}

func TestSettingsAcceptAutomaticTargetLanguage(test *testing.T) {
	application := newIntegrationServer(test)
	settings, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	settings.SourceLanguage = "auto"
	settings.TargetLanguage = "auto"
	response := performRequest(application, "PUT", "/api/settings", jsonText(test, settings), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("automatic target settings rejected: %d %s", response.Code, response.Body.String())
	}
	persisted, err := application.store.GetSettings(context.Background())
	if err != nil || persisted.TargetLanguage != "auto" {
		test.Fatalf("automatic target setting was not persisted: %+v %v", persisted, err)
	}
}

func TestCredentialSaveFailureDoesNotLeaveConfiguration(t *testing.T) {
	application := newIntegrationServer(t)
	application.options.Credentials.(*memoryCredentials).failWrite = true
	key := "not-persisted"
	configuration := providerSave{ProviderConfig: domain.ProviderConfig{Name: "Test", Type: "openai-compatible", BaseURL: "https://example.test/v1", Model: "test", TimeoutSeconds: 10, Enabled: true}, APIKey: &key}
	response := performRequest(application, "POST", "/api/providers", jsonText(t, configuration), testOrigin)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("credential failure: %d", response.Code)
	}
	providers, err := application.store.ListProviders(context.Background())
	if err != nil || len(providers) != 1 || providers[0].ID != domain.BuiltInMicrosoftProviderID {
		t.Fatalf("orphan provider after credential failure: %v %v", providers, err)
	}
}
