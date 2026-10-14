package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cvtranslate/internal/domain"
)

func microsoftTestConfig() domain.ProviderConfig {
	return domain.ProviderConfig{Name: "Microsoft Translator", Type: "microsoft-translate", TimeoutSeconds: 1, Enabled: true}
}

func TestMicrosoftProtocol(test *testing.T) {
	for _, source := range []string{"auto", "en", "zh-CN"} {
		for _, target := range []string{"en", "zh-CN"} {
			test.Run(source+"-"+target, func(test *testing.T) {
				input := TranslationRequest{Text: `<tag>&amp; "quote" 'apostrophe'`, SourceLanguage: source, TargetLanguage: target, SystemPrompt: "must not send", UserPrompt: "must not send"}
				mapped := map[string]string{"auto": "", "en": "en", "zh-CN": "zh-Hans"}
				upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					query := request.URL.Query()
					if request.Method != "POST" || request.URL.Path != "/translate/translatetext" || !query.Has("from") || query.Get("from") != mapped[source] || query.Get("to") != mapped[target] || query.Get("isEnterpriseClient") != "false" || len(query) != 3 {
						test.Error("unexpected protocol routing")
					}
					if request.Header.Get("Authorization") != "" || request.Header.Get("Content-Type") != "application/json" {
						test.Error("unexpected protocol headers")
					}
					var body []string
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body) != 1 || body[0] != html.EscapeString(input.Text) {
						test.Errorf("incorrect escaping/body: %v", err)
					}
					fmt.Fprintf(writer, `[{"translations":[{"text":" &lt;tag&gt;&amp;amp; ","to":%q}]}]`, mapped[target])
				}))
				defer upstream.Close()
				output, err := translateMicrosoft(context.Background(), microsoftTestConfig(), input, upstream.Client(), upstream.URL+"/translate/translatetext")
				if err != nil || output != " <tag>&amp; " {
					test.Fatalf("decode exactly once: %q %v", output, err)
				}
			})
		}
	}
}

func TestMicrosoftResponses(test *testing.T) {
	for _, scenario := range []struct {
		name, body, code string
		status           int
	}{
		{"malformed", "private source", "invalid_response", 200},
		{"truncated", `[{"translations":[{"text":"partial`, "invalid_response", 200},
		{"trailing", `[{"translations":[{"text":"ok","to":"en"}]}] {}`, "invalid_response", 200},
		{"empty array", `[]`, "invalid_response", 200},
		{"multiple results", `[{},{}]`, "invalid_response", 200},
		{"missing translations", `[{}]`, "invalid_response", 200},
		{"empty text", `[{"translations":[{"text":" &#32; ","to":"en"}]}]`, "incomplete_response", 200},
		{"wrong language", `[{"translations":[{"text":"ok","to":"zh-Hans"}]}]`, "incomplete_response", 200},
		{"oversized", strings.Repeat("x", 2*1024*1024+1), "invalid_response", 200},
		{"unauthorized", "private source", "unauthorized", 401},
		{"forbidden", "private source", "unauthorized", 403},
		{"rate limit", "private source", "rate_limited", 429},
		{"upstream", "private source", "upstream_error", 500},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.WriteHeader(scenario.status)
				fmt.Fprint(writer, scenario.body)
			}))
			defer upstream.Close()
			_, err := translateMicrosoft(context.Background(), microsoftTestConfig(), TranslationRequest{Text: "source", SourceLanguage: "auto", TargetLanguage: "en"}, upstream.Client(), upstream.URL)
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != scenario.code || strings.Contains(err.Error(), "private source") || calls.Load() != 1 {
				test.Fatalf("unsafe/incorrect failure or retry: %v", err)
			}
		})
	}
}

func TestMicrosoftRedirectAndCancellation(test *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, 307)
	}))
	defer upstream.Close()
	input := TranslationRequest{Text: "source", SourceLanguage: "auto", TargetLanguage: "en"}
	if _, err := translateMicrosoft(context.Background(), microsoftTestConfig(), input, upstream.Client(), upstream.URL); err == nil || redirected.Load() != 0 {
		test.Fatal("redirect followed")
	}
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := translateMicrosoft(requestContext, microsoftTestConfig(), input, upstream.Client(), upstream.URL)
	var safe *Error
	if !errors.As(err, &safe) || safe.Code != "cancelled" {
		test.Fatalf("cancellation: %v", err)
	}
}

func TestMicrosoftTimeoutIncludesBody(test *testing.T) {
	for _, sendHeaders := range []bool{false, true} {
		test.Run(fmt.Sprint(sendHeaders), func(test *testing.T) {
			release := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if sendHeaders {
					writer.WriteHeader(200)
					writer.(http.Flusher).Flush()
				}
				<-release
			}))
			defer upstream.Close()
			_, err := translateMicrosoft(context.Background(), microsoftTestConfig(), TranslationRequest{Text: "source", SourceLanguage: "en", TargetLanguage: "en"}, upstream.Client(), upstream.URL)
			close(release)
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != "timeout" {
				test.Fatalf("timeout: %v", err)
			}
		})
	}
}

func TestMicrosoftValidationAndCapabilities(test *testing.T) {
	configuration := microsoftTestConfig()
	capabilities, exists := CapabilitiesFor(configuration.Type)
	if !exists || capabilities.SupportsAPIKey || capabilities.SupportsPrompts || capabilities.ConfigurableEndpoint || capabilities.RequiresModel {
		test.Fatal("incorrect Microsoft capabilities")
	}
	openAI, _ := CapabilitiesFor("openai-compatible")
	if !openAI.SupportsAPIKey || !openAI.SupportsPrompts || !openAI.ConfigurableEndpoint || !openAI.RequiresModel {
		test.Fatal("incorrect OpenAI capabilities")
	}
	for _, mutate := range []func(*domain.ProviderConfig){
		func(config *domain.ProviderConfig) { config.BaseURL = "https://override.example" },
		func(config *domain.ProviderConfig) { config.Model = "model" },
		func(config *domain.ProviderConfig) { config.Name = " " },
		func(config *domain.ProviderConfig) { config.TimeoutSeconds = 0 },
		func(config *domain.ProviderConfig) { config.TimeoutSeconds = 301 },
	} {
		invalid := configuration
		mutate(&invalid)
		if ValidateConfig(invalid) == nil {
			test.Fatal("invalid configuration accepted")
		}
	}
	for _, input := range []TranslationRequest{
		{Text: "", SourceLanguage: "auto", TargetLanguage: "en"},
		{Text: "\xff", SourceLanguage: "auto", TargetLanguage: "en"},
		{Text: strings.Repeat("x", domain.MaxTextRunes+1), SourceLanguage: "auto", TargetLanguage: "en"},
		{Text: "text", SourceLanguage: "unknown", TargetLanguage: "en"},
		{Text: "text", SourceLanguage: "en", TargetLanguage: "auto"},
	} {
		if _, err := translateMicrosoft(context.Background(), configuration, input, nil, ""); err == nil {
			test.Fatal("invalid input accepted")
		}
	}
	if _, err := Translate(context.Background(), configuration, "secret", TranslationRequest{}); err == nil {
		test.Fatal("key accepted")
	}
}
