package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/speech"
)

func TestSpeechPreviewUsesDraftCatalogVoiceWithoutSaving(test *testing.T) {
	var received speech.Input
	application := newSpeechServer(test, speechFunction(func(_ context.Context, input speech.Input) ([]byte, error) {
		received = input
		return []byte("preview audio"), nil
	}))
	settings, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	// A draft Edge preview is independent of the engine saved for workspace text.
	settings.Speech.Engine = "web-speech"
	if err := application.store.SaveSettings(context.Background(), settings); err != nil {
		test.Fatal(err)
	}
	application.options.ConfigureHotkeys = func(_ domain.Hotkeys) error {
		test.Error("preview must not configure hotkeys")
		return nil
	}
	for _, voice := range speech.Voices() {
		body := jsonText(test, map[string]string{"language": voice.Language, "voiceId": voice.ID})
		response := performRequest(application, "POST", "/api/speech/preview", body, testOrigin)
		if response.Code != http.StatusOK || response.Body.String() != "preview audio" || response.Header().Get("Content-Type") != "audio/mpeg" || response.Header().Get("Cache-Control") != "no-store" {
			test.Fatalf("preview response failed: %d %s", response.Code, response.Body.String())
		}
		expectedText := "Hello! This is a preview of the English reading voice."
		if voice.Language == "zh-CN" {
			expectedText = "\u4f60\u597d\uff01\u8fd9\u662f\u7b80\u4f53\u4e2d\u6587\u6717\u8bfb\u58f0\u97f3\u7684\u8bd5\u542c\u3002"
		}
		if received.Text != expectedText || received.Language != voice.Language || received.Voice != voice.ID {
			test.Fatalf("preview did not use the requested catalog voice and fixed sample: %+v", received)
		}
	}
	actual, err := application.store.GetSettings(context.Background())
	if err != nil || actual != settings {
		test.Fatalf("preview changed saved settings: %+v %v", actual, err)
	}
	response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"en"}`, testOrigin)
	if response.Code != http.StatusConflict {
		test.Fatalf("preview bypassed the saved engine for workspace reading: %d", response.Code)
	}
}

func TestSpeechPreviewRejectsUntrustedInputsBeforeSynthesis(test *testing.T) {
	application := newSpeechServer(test, speechFunction(func(context.Context, speech.Input) ([]byte, error) {
		test.Error("invalid preview reached synthesis")
		return nil, nil
	}))
	for _, body := range []string{
		`{}`,
		`{"language":"auto","voiceId":"en-US-AriaNeural"}`,
		`{"language":"fr","voiceId":"en-US-AriaNeural"}`,
		`{"language":"en","voiceId":"zh-CN-YunxiNeural"}`,
		`{"language":"zh-CN","voiceId":"en-US-AriaNeural"}`,
		`{"language":"en","voiceId":""}`,
		`{"language":"en","voiceId":"private-injected-value"}`,
		`{"language":"en","voiceId":"en-US-AriaNeural","text":"private-injected-value"}`,
		`{"language":"en","voiceId":"en-US-AriaNeural","ssml":"private-injected-value"}`,
		`{"language":"en","voiceId":"en-US-AriaNeural","endpoint":"https://private-injected-value"}`,
		`{"language":"en","voiceId":"en-US-AriaNeural","engine":"edge"}`,
	} {
		response := performRequest(application, "POST", "/api/speech/preview", body, testOrigin)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-injected-value") {
			test.Fatalf("unsafe preview response: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestSpeechPreviewRetainsBrowserOriginBoundary(test *testing.T) {
	application := newSpeechServer(test, speechFunction(func(context.Context, speech.Input) ([]byte, error) {
		test.Error("untrusted preview request reached synthesis")
		return nil, nil
	}))
	for _, scenario := range []struct {
		name, origin, host, fetchSite string
	}{
		{name: "missing origin"},
		{name: "foreign origin", origin: "https://other.example"},
		{name: "opaque origin", origin: "null"},
		{name: "foreign host", origin: testOrigin, host: "other.example"},
		{name: "cross site", origin: testOrigin, fetchSite: "cross-site"},
		{name: "same site cross origin", origin: testOrigin, fetchSite: "same-site"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			request := httptest.NewRequest("POST", testOrigin+"/api/speech/preview", strings.NewReader(`{"language":"en","voiceId":"en-US-AriaNeural"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", scenario.origin)
			request.Header.Set("Sec-Fetch-Site", scenario.fetchSite)
			if scenario.host != "" {
				request.Host = scenario.host
			}
			response := httptest.NewRecorder()
			application.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				test.Fatalf("untrusted preview accepted: %d", response.Code)
			}
		})
	}
	response := performRequest(application, "GET", "/api/speech/preview", "", "")
	if response.Code != http.StatusNotFound {
		test.Fatalf("preview must not synthesize through GET: %d", response.Code)
	}
}

func TestSpeechPreviewPropagatesCancellation(test *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	application := newSpeechServer(test, speechFunction(func(ctx context.Context, _ speech.Input) ([]byte, error) {
		if ctx != requestContext {
			test.Error("preview lost the HTTP context")
		}
		cancel()
		if ctx.Err() != context.Canceled {
			test.Error("preview did not propagate cancellation")
		}
		return nil, &speech.Error{Code: "cancelled", Message: "Cancelled."}
	}))
	request := httptest.NewRequest("POST", testOrigin+"/api/speech/preview", strings.NewReader(`{"language":"en","voiceId":"en-US-AriaNeural"}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	response := httptest.NewRecorder()
	application.ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout {
		test.Fatalf("cancelled preview returned %d", response.Code)
	}
}
