package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
)

type failingCredentials struct{}

func (failingCredentials) Get(string) (string, error) { return "", errors.New("private keyring error") }
func (failingCredentials) Has(string) (bool, error) {
	return false, errors.New("private keyring error")
}
func (failingCredentials) Set(string, string) error { return errors.New("private keyring error") }
func (failingCredentials) Delete(string) error      { return errors.New("private keyring error") }

type microsoftTestTransport func(*http.Request) (*http.Response, error)

func (transport microsoftTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestMicrosoftConfigurationWithoutKeyring(test *testing.T) {
	for _, credentials := range []Credentials{nil, failingCredentials{}} {
		application := newIntegrationServer(test)
		application.options.Credentials = credentials
		response := performRequest(application, "GET", "/api/state", "", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"supportsApiKey":false`) {
			test.Fatalf("state needs keyring: %d %s", response.Code, response.Body.String())
		}
		builtin, err := application.store.GetProvider(context.Background(), domain.BuiltInMicrosoftProviderID)
		if err != nil {
			test.Fatal(err)
		}
		builtin.Name = "Renamed Microsoft"
		response = performRequest(application, "POST", "/api/providers", jsonText(test, builtin), testOrigin)
		if response.Code != http.StatusOK {
			test.Fatalf("save needs keyring: %d", response.Code)
		}
		for _, input := range []providerSave{
			{ProviderConfig: builtin, ClearAPIKey: true},
			{ProviderConfig: builtin, APIKey: stringPointer("secret")},
		} {
			response = performRequest(application, "POST", "/api/providers", jsonText(test, input), testOrigin)
			if response.Code != http.StatusBadRequest {
				test.Fatal("Microsoft accepted credential mutation")
			}
		}
		builtin.Enabled = false
		response = performRequest(application, "POST", "/api/providers", jsonText(test, builtin), testOrigin)
		if response.Code != http.StatusConflict {
			test.Fatal("disabled default")
		}
		response = performRequest(application, "DELETE", "/api/providers/"+builtin.ID, "", testOrigin)
		if response.Code != http.StatusConflict {
			test.Fatal("deleted default")
		}
		builtin.ID = ""
		builtin.Enabled = true
		response = performRequest(application, "POST", "/api/providers", jsonText(test, builtin), testOrigin)
		if response.Code != http.StatusOK {
			test.Fatal("creation needs keyring")
		}
		var additional domain.ProviderConfig
		if err := json.Unmarshal(response.Body.Bytes(), &additional); err != nil {
			test.Fatal(err)
		}
		response = performRequest(application, "DELETE", "/api/providers/"+additional.ID, "", testOrigin)
		if response.Code != http.StatusNoContent {
			test.Fatalf("delete needs keyring: %d", response.Code)
		}
	}
}

func stringPointer(value string) *string { return &value }

func TestProviderTypeImmutablePreservesSecret(test *testing.T) {
	application := newIntegrationServer(test)
	configuration := domain.ProviderConfig{ID: "existing", Name: "Existing", Type: "openai-compatible", BaseURL: "https://example.test", Model: "model", TimeoutSeconds: 30, Enabled: true}
	if err := application.store.SaveProvider(context.Background(), configuration); err != nil {
		test.Fatal(err)
	}
	if err := application.options.Credentials.Set(configuration.ID, "existing-secret"); err != nil {
		test.Fatal(err)
	}
	changed := configuration
	changed.Type = "microsoft-translate"
	changed.BaseURL = ""
	changed.Model = ""
	response := performRequest(application, "POST", "/api/providers", jsonText(test, changed), testOrigin)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "provider_type_immutable") {
		test.Fatal("type change accepted")
	}
	actual, err := application.store.GetProvider(context.Background(), configuration.ID)
	if err != nil || actual != configuration {
		test.Fatal("original configuration lost")
	}
	key, err := application.options.Credentials.Get(configuration.ID)
	if err != nil || key != "existing-secret" {
		test.Fatal("original key lost")
	}
}

func TestOpenAIConfigurationFailsClosedWithoutKeyring(test *testing.T) {
	for _, credentials := range []Credentials{nil, failingCredentials{}} {
		application := newIntegrationServer(test)
		configuration := domain.ProviderConfig{ID: "existing", Name: "Existing", Type: "openai-compatible", BaseURL: "https://example.test", Model: "model", TimeoutSeconds: 30, Enabled: true}
		if err := application.store.SaveProvider(context.Background(), configuration); err != nil {
			test.Fatal(err)
		}
		application.options.Credentials = credentials
		for _, operation := range []struct{ method, path, body string }{
			{"GET", "/api/state", ""},
			{"POST", "/api/providers", jsonText(test, configuration)},
			{"DELETE", "/api/providers/existing", ""},
			{"POST", "/api/providers/existing/test", ""},
		} {
			response := performRequest(application, operation.method, operation.path, operation.body, testOrigin)
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private keyring error") {
				test.Fatalf("credential failure hidden/leaked: %s %d", operation.path, response.Code)
			}
		}
		actual, err := application.store.GetProvider(context.Background(), configuration.ID)
		if err != nil || actual != configuration {
			test.Fatal("failed credential access changed configuration")
		}
	}
}

func TestMicrosoftConnectionTestSkipsKeyring(test *testing.T) {
	// Intercept the standard transport only for this non-parallel test. The
	// adapter's production endpoint stays fixed and no live request is made.
	previousTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = previousTransport }()
	var calls int
	http.DefaultTransport = microsoftTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Host != "edge.microsoft.com" || request.URL.Path != "/translate/translatetext" || request.Header.Get("Authorization") != "" {
			test.Error("unexpected Microsoft request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"translations":[{"text":"Hello.","to":"en"}]}]`)), Request: request}, nil
	})
	for _, credentials := range []Credentials{nil, failingCredentials{}} {
		application := newIntegrationServer(test)
		application.options.Credentials = credentials
		response := performRequest(application, "POST", "/api/providers/builtin-microsoft/test", "", testOrigin)
		if response.Code != http.StatusOK {
			test.Fatalf("test needs keyring: %d %s", response.Code, response.Body.String())
		}
	}
	if calls != 2 {
		test.Fatal("unexpected calls")
	}
}
