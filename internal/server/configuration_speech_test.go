package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/speech"
)

func TestSpeechSettingsSaveAndStateMetadataWithoutNetwork(test *testing.T) {
	previousTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = previousTransport }()
	http.DefaultTransport = microsoftTestTransport(func(*http.Request) (*http.Response, error) {
		test.Error("saving speech settings or loading metadata contacted upstream")
		return nil, errors.New("unexpected upstream request")
	})
	application := newSpeechServer(test, nil)
	response := performRequest(application, "GET", "/api/state", "", "")
	if response.Code != http.StatusOK {
		test.Fatalf("state failed: %d %s", response.Code, response.Body.String())
	}
	var state struct {
		Settings     domain.Settings `json:"settings"`
		SpeechVoices []speech.Voice  `json:"speechVoices"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		test.Fatal(err)
	}
	if state.Settings.Speech != domain.DefaultSpeechSettings() || !reflect.DeepEqual(state.SpeechVoices, speech.Voices()) {
		test.Fatalf("speech defaults or local metadata missing: %+v", state)
	}
	expectedLanguages := map[string]string{
		"en-US-AndrewMultilingualNeural": "en", "en-US-AriaNeural": "en", "en-US-GuyNeural": "en", "en-US-JennyNeural": "en",
		"zh-CN-XiaoxiaoMultilingualNeural": "zh-CN", "zh-CN-XiaoxiaoNeural": "zh-CN", "zh-CN-XiaoyiNeural": "zh-CN", "zh-CN-YunxiNeural": "zh-CN",
	}
	for _, voice := range state.SpeechVoices {
		if voice.Name == "" || expectedLanguages[voice.ID] != voice.Language {
			test.Fatalf("invalid catalog metadata: %+v", voice)
		}
		delete(expectedLanguages, voice.ID)
	}
	if len(expectedLanguages) != 0 {
		test.Fatalf("required voices missing: %v", expectedLanguages)
	}
	settings := state.Settings
	settings.Speech.EdgeVoices = domain.SpeechVoices{English: "en-US-AriaNeural", Chinese: "zh-CN-YunxiNeural"}
	// Browser voiceURI values are opaque data, even when they resemble URLs.
	settings.Speech.WebSpeechVoices = domain.SpeechVoices{English: "https://browser-voice.example/opaque", Chinese: "local-chinese-voice"}
	for _, engine := range []string{"web-speech", "edge"} {
		settings.Speech.Engine = engine
		response = performRequest(application, "PUT", "/api/settings", jsonText(test, settings), testOrigin)
		if response.Code != http.StatusOK {
			test.Fatalf("save failed: %d %s", response.Code, response.Body.String())
		}
		var saved domain.Settings
		if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved != settings {
			test.Fatalf("save response lost engine or inactive voices: %+v %v", saved, err)
		}
		response = performRequest(application, "GET", "/api/state", "", "")
		if response.Code != http.StatusOK {
			test.Fatalf("state after save failed: %d", response.Code)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.Settings != settings {
			test.Fatalf("state did not preserve saved choices: %+v %v", state.Settings, err)
		}
	}
}

func TestInvalidSpeechSettingsPreservePersistenceAndHotkeys(test *testing.T) {
	application := newSpeechServer(test, nil)
	previous, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	configured := false
	application.options.ConfigureHotkeys = func(domain.Hotkeys) error {
		configured = true
		return nil
	}
	for _, scenario := range []struct {
		name   string
		change func(*domain.SpeechSettings)
	}{
		{name: "missing speech", change: func(settings *domain.SpeechSettings) { *settings = domain.SpeechSettings{} }},
		{name: "unknown engine", change: func(settings *domain.SpeechSettings) { settings.Engine = "private-attacker-value" }},
		{name: "unknown edge voice", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = "private-attacker-value" }},
		{name: "missing edge voice", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.Chinese = "" }},
		{name: "mismatched language", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.Chinese = settings.EdgeVoices.English }},
		{name: "invalid inactive edge voice", change: func(settings *domain.SpeechSettings) {
			settings.Engine = "web-speech"
			settings.EdgeVoices.English = "private-attacker-value"
		}},
		{name: "invalid inactive browser voice", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.English = "private-attacker-value\n" }},
		{name: "oversized browser voice", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.Chinese = strings.Repeat("a", 1025) }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			changed := previous
			changed.Hotkeys.OpenUI = "Ctrl+F8"
			scenario.change(&changed.Speech)
			response := performRequest(application, "PUT", "/api/settings", jsonText(test, changed), testOrigin)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_speech_settings"`) || strings.Contains(response.Body.String(), "private-attacker-value") {
				test.Fatalf("unsafe settings response: %d %s", response.Code, response.Body.String())
			}
			actual, err := application.store.GetSettings(context.Background())
			if err != nil || actual != previous || configured {
				test.Fatalf("invalid speech settings mutated persistence or hotkeys: configured=%v error=%v", configured, err)
			}
		})
	}
}

func TestSpeechRoutesSavedVoiceForExplicitAndAutomaticLanguage(test *testing.T) {
	var received speech.Input
	application := newSpeechServer(test, speechFunction(func(_ context.Context, input speech.Input) ([]byte, error) {
		received = input
		return []byte("audio"), nil
	}))
	settings, err := application.store.GetSettings(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	for _, choices := range []domain.SpeechVoices{
		domain.DefaultSpeechSettings().EdgeVoices,
		{English: "en-US-JennyNeural", Chinese: "zh-CN-XiaoyiNeural"},
	} {
		settings.Speech.EdgeVoices = choices
		response := performRequest(application, "PUT", "/api/settings", jsonText(test, settings), testOrigin)
		if response.Code != http.StatusOK {
			test.Fatalf("could not save selected voices: %d", response.Code)
		}
		for _, scenario := range []struct {
			text, language, expectedVoice string
		}{
			{text: "hello", language: "en", expectedVoice: choices.English},
			{text: "\u4e2d\u6587", language: "zh-CN", expectedVoice: choices.Chinese},
			{text: "hello", language: "auto", expectedVoice: choices.English},
			{text: "hello \u4e2d\u6587", language: "auto", expectedVoice: choices.Chinese},
			{text: "\u4e2d\u6587", language: "en", expectedVoice: choices.English},
		} {
			body := jsonText(test, speech.Input{Text: scenario.text, Language: scenario.language})
			response = performRequest(application, "POST", "/api/speech", body, testOrigin)
			if response.Code != http.StatusOK || received.Text != scenario.text || received.Language != scenario.language || received.Voice != scenario.expectedVoice {
				test.Fatalf("saved voice routing failed: status=%d input=%+v expected=%s", response.Code, received, scenario.expectedVoice)
			}
		}
	}
}

func TestSpeechRejectsBrowserEngineOrInvalidSavedSettingsBeforeSynthesis(test *testing.T) {
	called := false
	application := newSpeechServer(test, speechFunction(func(context.Context, speech.Input) ([]byte, error) {
		called = true
		return []byte("audio"), nil
	}))
	for _, scenario := range []struct {
		name, code string
		change     func(*domain.SpeechSettings)
	}{
		{name: "browser engine", code: "speech_engine_mismatch", change: func(settings *domain.SpeechSettings) { settings.Engine = "web-speech" }},
		{name: "unknown engine", code: "invalid_speech_settings", change: func(settings *domain.SpeechSettings) { settings.Engine = "private-saved-value" }},
		{name: "missing voice", code: "invalid_speech_settings", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = "" }},
		{name: "unknown voice", code: "invalid_speech_settings", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = "private-saved-value" }},
		{name: "mismatched voice", code: "invalid_speech_settings", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = settings.EdgeVoices.Chinese }},
		{name: "invalid inactive browser voice", code: "invalid_speech_settings", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.Chinese = "private-saved-value\n" }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			settings := domain.DefaultSettings()
			scenario.change(&settings.Speech)
			if err := application.store.SaveSettings(context.Background(), settings); err != nil {
				test.Fatal(err)
			}
			response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"en"}`, testOrigin)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"`+scenario.code+`"`) || called || strings.Contains(response.Body.String(), "private-saved-value") {
				test.Fatalf("unsafe stored speech settings reached synthesis: %d %s called=%v", response.Code, response.Body.String(), called)
			}
		})
	}
}

func TestSpeechReleasesConfigurationMutexAfterSnapshot(test *testing.T) {
	application := newSpeechServer(test, nil)
	settings := domain.DefaultSettings()
	settings.Speech.EdgeVoices.English = "en-US-GuyNeural"
	application.options.Speech = speechFunction(func(_ context.Context, input speech.Input) ([]byte, error) {
		if !application.configurationMutex.TryLock() {
			test.Error("synthesis retained the configuration mutex")
			return nil, errors.New("configuration still locked")
		}
		application.configurationMutex.Unlock()
		response := performRequest(application, "PUT", "/api/settings", jsonText(test, settings), testOrigin)
		if response.Code != http.StatusOK || input.Voice != domain.DefaultSpeechSettings().EdgeVoices.English {
			test.Errorf("concurrent settings save blocked or replaced captured voice: status=%d input=%+v", response.Code, input)
		}
		return []byte("audio"), nil
	})
	response := performRequest(application, "POST", "/api/speech", `{"text":"hello","language":"en"}`, testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("snapshot synthesis failed: %d %s", response.Code, response.Body.String())
	}
	actual, err := application.store.GetSettings(context.Background())
	if err != nil || actual != settings {
		test.Fatalf("settings edit during synthesis was not persisted: %+v %v", actual, err)
	}
}
