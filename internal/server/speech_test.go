package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/speech"
	"cvtranslate/internal/storage"
)

type speechFunction func(context.Context, speech.Input) ([]byte, error)

func (synthesize speechFunction) Synthesize(ctx context.Context, input speech.Input) ([]byte, error) {
	return synthesize(ctx, input)
}

func newSpeechServer(test *testing.T, synthesizer SpeechSynthesizer) *Server {
	test.Helper()
	repository, err := storage.Open(filepath.Join(test.TempDir(), "speech.sqlite"))
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = repository.Close() })
	return New(repository, nil, Options{Origin: testOrigin, Speech: synthesizer})
}

func TestSpeechRequiresLocalOrigin(test *testing.T) {
	called := false
	application := newSpeechServer(test, speechFunction(func(context.Context, speech.Input) ([]byte, error) {
		called = true
		return []byte("audio"), nil
	}))
	for _, requestCase := range []struct {
		name   string
		origin string
	}{
		{name: "missing origin"},
		{name: "foreign origin", origin: "https://other.example"},
		{name: "opaque origin", origin: "null"},
		{name: "another local port", origin: "http://127.0.0.1:19329"},
	} {
		test.Run(requestCase.name, func(test *testing.T) {
			response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"en"}`, requestCase.origin)
			if response.Code != http.StatusForbidden || called {
				test.Fatalf("speech origin boundary failed: status=%d called=%v", response.Code, called)
			}
		})
	}
	response := performRequest(application, "GET", "/api/speech?text=hello", "", "")
	if response.Code != http.StatusNotFound || called {
		test.Fatal("speech must not send text through a GET request")
	}
}

func TestSpeechReturnsOnlyUncachedAudio(test *testing.T) {
	application := newSpeechServer(test, speechFunction(func(ctx context.Context, input speech.Input) ([]byte, error) {
		if input.Text != "hello" || input.Language != "auto" || input.Voice != domain.DefaultSpeechSettings().EdgeVoices.English || ctx.Err() != nil {
			test.Errorf("speech input was altered: %+v", input)
		}
		return []byte("test mp3 bytes"), nil
	}))
	response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"auto"}`, testOrigin)
	if response.Code != http.StatusOK || response.Body.String() != "test mp3 bytes" || response.Header().Get("Content-Type") != "audio/mpeg" {
		test.Fatalf("unexpected audio response: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Length") != "14" {
		test.Fatal("audio caching or length headers are missing")
	}
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "media-src 'self' blob:;") || !strings.Contains(policy, "connect-src 'self';") {
		test.Fatal("audio playback changed unrelated network access boundaries")
	}
}

func TestSpeechRejectsEndpointInjectionAndInvalidTextBeforeNetwork(test *testing.T) {
	previousTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = previousTransport }()
	http.DefaultTransport = microsoftTestTransport(func(*http.Request) (*http.Response, error) {
		test.Error("invalid speech input reached network")
		return nil, errors.New("unexpected speech network request")
	})
	application := newSpeechServer(test, speech.New())
	for _, body := range []string{
		`{"text":"hello","language":"en","endpoint":"https://untrusted.example"}`,
		`{"text":"hello","language":"en","url":"https://untrusted.example"}`,
		`{"text":"hello","language":"en","engine":"edge"}`,
		`{"text":"hello","language":"en","voice":"injected"}`,
		`{"text":"hello","language":"en","voice":"en-US-JennyNeural"}`,
		`{"text":"","language":"en"}`,
		`{"text":"hello","language":"unsupported"}`,
		`{"text":"hello\u0001","language":"en"}`,
	} {
		response := performRequest(application, "POST", "/api/speech", body, testOrigin)
		if response.Code != http.StatusBadRequest {
			test.Fatalf("unsafe speech input accepted: %d", response.Code)
		}
	}
}

func TestSpeechSafeErrorsAndContextPropagation(test *testing.T) {
	for _, errorCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "untrusted error", err: errors.New("private-token-and-source-text"), status: http.StatusBadGateway},
		{name: "timeout", err: &speech.Error{Code: "timeout", Message: "Timed out."}, status: http.StatusGatewayTimeout},
		{name: "busy", err: &speech.Error{Code: "speech_busy", Message: "Busy."}, status: http.StatusTooManyRequests},
		{name: "cancelled", err: &speech.Error{Code: "cancelled", Message: "Cancelled."}, status: http.StatusRequestTimeout},
	} {
		test.Run(errorCase.name, func(test *testing.T) {
			application := newSpeechServer(test, speechFunction(func(context.Context, speech.Input) ([]byte, error) {
				return nil, errorCase.err
			}))
			response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"en"}`, testOrigin)
			if response.Code != errorCase.status || strings.Contains(response.Body.String(), "private-token-and-source-text") {
				test.Fatalf("unsafe speech error: %d %s", response.Code, response.Body.String())
			}
		})
	}
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	application := newSpeechServer(test, speechFunction(func(ctx context.Context, _ speech.Input) ([]byte, error) {
		called = true
		if ctx != requestContext {
			test.Error("HTTP context did not reach synthesis")
		}
		cancel()
		if ctx.Err() != context.Canceled {
			test.Error("HTTP cancellation did not reach synthesis")
		}
		return nil, &speech.Error{Code: "cancelled", Message: "Cancelled."}
	}))
	request := httptest.NewRequest("POST", testOrigin+"/api/speech", strings.NewReader(`{"text":"hello","language":"en"}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	response := httptest.NewRecorder()
	application.ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout || !called {
		test.Fatalf("cancelled synthesis returned %d", response.Code)
	}
}
