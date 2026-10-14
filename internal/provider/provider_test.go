package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResponses(t *testing.T) {
	testCases := []struct {
		name, body, code string
		status           int
	}{
		{"success", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":" translated\n"}}]}`, "", 200},
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"partial"}}]}`, "incomplete_response", 200},
		{"refusal", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"no","refusal":"private upstream text"}}]}`, "incomplete_response", 200},
		{"tools", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"no","tool_calls":[{}]}}]}`, "incomplete_response", 200},
		{"empty", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":" "}}]}`, "incomplete_response", 200},
		{"malformed", "private upstream text", "invalid_response", 200},
		{"unauthorized", "private upstream text", "unauthorized", 401},
		{"rate limit", "private upstream text", "rate_limited", 429},
		{"oversized", strings.Repeat("x", 2*1024*1024+1), "invalid_response", 200},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer secret" {
					t.Error("incorrect request endpoint or authorization")
				}
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["stream"] != false || len(body) != 3 {
					t.Error("unexpected request fields")
				}
				writer.WriteHeader(testCase.status)
				fmt.Fprint(writer, testCase.body)
			}))
			defer server.Close()
			config := testConfig(server.URL + "/v1")
			output, err := Translate(context.Background(), config, "secret", TranslationRequest{Text: "user", SystemPrompt: "system", UserPrompt: "user"})
			if testCase.code == "" {
				if err != nil || output != " translated\n" {
					t.Fatalf("output=%q err=%v", output, err)
				}
				return
			}
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != testCase.code {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
				t.Fatal("sensitive error")
			}
		})
	}
}

func TestOpenAIUsageAndDefaultModel(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"translated"}}],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`)
	}))
	defer server.Close()
	result, err := TranslateWithUsage(context.Background(), testConfig(server.URL), "", TranslationRequest{Text: "source", SystemPrompt: "system", UserPrompt: "user"})
	if err != nil || result.Text != "translated" || result.TokenUsage == nil {
		test.Fatalf("usage response failed: %+v %v", result, err)
	}
	expectedUsage := struct{ prompt, completion, total int64 }{120, 30, 150}
	if result.TokenUsage.PromptTokens != expectedUsage.prompt || result.TokenUsage.CompletionTokens != expectedUsage.completion || result.TokenUsage.TotalTokens != expectedUsage.total {
		test.Fatalf("usage was not preserved: %+v", result.TokenUsage)
	}
	capabilities, exists := CapabilitiesFor("openai-compatible")
	if !exists || capabilities.DefaultModel != "gpt-6-luna" {
		test.Fatalf("default model metadata = %+v", capabilities)
	}
}

func TestOpenAIInvalidUsageDoesNotDiscardTranslation(test *testing.T) {
	invalidUsageValues := []string{
		`{"prompt_tokens":20,"completion_tokens":10,"total_tokens":5}`,
		`{"prompt_tokens":"unknown","completion_tokens":10,"total_tokens":30}`,
		`{}`,
	}
	for _, invalidUsage := range invalidUsageValues {
		test.Run(invalidUsage, func(test *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(writer, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"translated"}}],"usage":%s}`, invalidUsage)
			}))
			defer server.Close()
			result, err := TranslateWithUsage(context.Background(), testConfig(server.URL), "", TranslationRequest{Text: "source", SystemPrompt: "system", UserPrompt: "user"})
			if err != nil || result.Text != "translated" || result.TokenUsage != nil {
				test.Fatalf("invalid optional usage changed translation: %+v %v", result, err)
			}
		})
	}
}

func TestRedirectDoesNotForwardKey(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, 307)
	}))
	defer origin.Close()
	if _, err := Translate(context.Background(), testConfig(origin.URL), "secret", TranslationRequest{Text: "user", SystemPrompt: "system", UserPrompt: "user"}); err == nil {
		t.Fatal("redirect succeeded")
	}
	if calls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestEndpointValidation(test *testing.T) {
	for _, endpoint := range []string{
		"https://example.com/v1",
		"http://example.com:8080/v1",
		"http://192.168.1.10:8080/v1",
		"http://[2001:db8::1]:8080/v1",
		"http://localhost:8080/v1",
		"http://127.0.0.1:8080/v1",
		"http://[::1]:8080/v1",
		"http://example.com/v1%3F%23",
	} {
		test.Run(endpoint, func(test *testing.T) {
			if err := ValidateConfig(testConfig(endpoint)); err != nil {
				test.Fatalf("valid HTTP(S) endpoint rejected: %v", err)
			}
		})
	}
	for _, endpoint := range []string{
		"",
		"/v1",
		"http://",
		"http://user:secret@example.com/v1",
		"https://user:secret@example.com/v1",
		"http://example.com/v1?key=secret",
		"https://example.com/v1?key=secret",
		"http://example.com/v1?",
		"http://example.com/v1#fragment",
		"https://example.com/v1#fragment",
		"http://example.com/v1#",
		"file:///tmp/test",
		"ftp://example.com/v1",
	} {
		test.Run(endpoint, func(test *testing.T) {
			err := ValidateConfig(testConfig(endpoint))
			var endpointError *Error
			if !errors.As(err, &endpointError) || endpointError.Code != "invalid_endpoint" {
				test.Fatalf("expected invalid_endpoint, got %v", err)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Translate(ctx, testConfig(server.URL), "", TranslationRequest{Text: "user", UserPrompt: "user"})
	var safe *Error
	if !errors.As(err, &safe) || safe.Code != "cancelled" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTimeoutIncludesResponseBody(t *testing.T) {
	for _, sendHeaders := range []bool{false, true} {
		t.Run(fmt.Sprintf("headers=%t", sendHeaders), func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if sendHeaders {
					writer.WriteHeader(http.StatusOK)
					writer.(http.Flusher).Flush()
				}
				<-release
			}))
			defer server.Close()
			config := testConfig(server.URL)
			config.TimeoutSeconds = 1
			_, err := Translate(context.Background(), config, "", TranslationRequest{Text: "user", SystemPrompt: "system", UserPrompt: "user"})
			close(release)
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != "timeout" {
				t.Fatalf("timeout incorrectly mapped: %v", err)
			}
		})
	}
}

func TestUnauthenticatedLocalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Error("unexpected authorization header")
		}
		fmt.Fprint(writer, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"translated"}}]}`)
	}))
	defer server.Close()
	if output, err := Translate(context.Background(), testConfig(server.URL), "", TranslationRequest{Text: "user", SystemPrompt: "system", UserPrompt: "user"}); err != nil || output != "translated" {
		t.Fatalf("local endpoint failed: %v", err)
	}
}

func TestUnknownProviderCannotDispatch(t *testing.T) {
	configuration := testConfig("https://unused.example/v1")
	configuration.Type = "unknown"
	if err := ValidateConfig(configuration); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := Translate(context.Background(), configuration, "", TranslationRequest{Text: "input"}); err == nil {
		t.Fatal("unknown provider dispatched")
	}
	if _, exists := CapabilitiesFor(configuration.Type); exists {
		t.Fatal("unknown provider advertised capabilities")
	}
	capabilities, exists := CapabilitiesFor("openai-compatible")
	if !exists || !capabilities.SupportsPrompts {
		t.Fatal("registered adapter capabilities unavailable")
	}
}
