package speech

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
)

func TestSettingsValidateActiveAndInactiveVoices(test *testing.T) {
	for _, engine := range []string{"edge", "web-speech"} {
		settings := domain.DefaultSpeechSettings()
		settings.Engine = engine
		if err := ValidateSettings(settings); err != nil {
			test.Fatalf("default %s settings rejected: %v", engine, err)
		}
		for _, scenario := range []struct {
			name   string
			change func(*domain.SpeechSettings)
		}{
			{name: "unknown engine", change: func(settings *domain.SpeechSettings) { settings.Engine = "unknown" }},
			{name: "empty engine", change: func(settings *domain.SpeechSettings) { settings.Engine = "" }},
			{name: "noncanonical engine", change: func(settings *domain.SpeechSettings) { settings.Engine = "Edge" }},
			{name: "missing english voice", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = "" }},
			{name: "unknown english voice", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = "arbitrary-private-voice" }},
			{name: "injected chinese voice", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.Chinese = `"><voice name="injected` }},
			{name: "english wrong language", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.English = settings.EdgeVoices.Chinese }},
			{name: "chinese wrong language", change: func(settings *domain.SpeechSettings) { settings.EdgeVoices.Chinese = settings.EdgeVoices.English }},
			{name: "browser english too long", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.English = strings.Repeat("a", 1025) }},
			{name: "browser chinese byte limit", change: func(settings *domain.SpeechSettings) {
				settings.WebSpeechVoices.Chinese = strings.Repeat("\u4e2d", 342)
			}},
			{name: "browser invalid utf8", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.English = "\xff" }},
			{name: "browser ascii control", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.English = "voice\nname" }},
			{name: "browser null control", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.Chinese = "voice\x00name" }},
			{name: "browser delete control", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.English = "voice\x7f" }},
			{name: "browser unicode control", change: func(settings *domain.SpeechSettings) { settings.WebSpeechVoices.Chinese = "voice\u0085" }},
		} {
			test.Run(engine+"/"+scenario.name, func(test *testing.T) {
				changed := settings
				scenario.change(&changed)
				requireSpeechError(test, ValidateSettings(changed), "invalid_speech_settings")
			})
		}
		for _, identifier := range []string{"", "Microsoft English Voice (en-US)", "https://voice.example/opaque#identifier", strings.Repeat("a", 1024), strings.Repeat("\u4e2d", 341) + "a"} {
			settings.WebSpeechVoices = domain.SpeechVoices{English: identifier, Chinese: identifier}
			if err := ValidateSettings(settings); err != nil {
				test.Fatalf("valid opaque browser identifier rejected: %v", err)
			}
		}
	}
}

func TestSelectedCatalogVoicesAppearInSSML(test *testing.T) {
	for _, selected := range Voices() {
		test.Run(selected.ID, func(test *testing.T) {
			calls := 0
			client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.String() == microsoftSpeechBootstrap {
					return speechResponse(200, "application/json", `{"t":"test-token","r":"eastus"}`), nil
				}
				var document struct {
					Language string `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
					Voice    struct {
						Name string `xml:"name,attr"`
					} `xml:"voice"`
				}
				if err := xml.NewDecoder(request.Body).Decode(&document); err != nil {
					test.Fatal(err)
				}
				locale := "en-US"
				if selected.Language == "zh-CN" {
					locale = "zh-CN"
				}
				if document.Voice.Name != selected.ID || document.Language != locale {
					test.Fatalf("saved voice or fixed locale ignored: %+v", document)
				}
				return speechResponse(200, "audio/mpeg", "chosen voice audio"), nil
			})
			service := New()
			service.synthesize = client.synthesize
			audio, err := service.Synthesize(context.Background(), Input{Text: "test text", Language: selected.Language, Voice: selected.ID})
			if err != nil || string(audio) != "chosen voice audio" || calls != 2 {
				test.Fatalf("selected voice synthesis failed: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestVoiceValidationRejectsUnknownOrMismatchedWithoutNetwork(test *testing.T) {
	service := New()
	service.synthesize = func(context.Context, Input, voice) ([]byte, error) {
		test.Fatal("invalid voice reached synthesis")
		return nil, nil
	}
	for _, input := range []Input{
		{Text: "hello", Language: "en", Voice: "unknown"},
		{Text: "hello", Language: "en", Voice: `"><voice name="injected`},
		{Text: "hello", Language: "en", Voice: "zh-CN-YunxiNeural"},
		{Text: "\u4e2d\u6587", Language: "zh-CN", Voice: "en-US-AriaNeural"},
		{Text: "\u4e2d\u6587", Language: "auto", Voice: "en-US-AriaNeural"},
		{Text: "hello", Language: "auto", Voice: "zh-CN-YunxiNeural"},
	} {
		_, err := service.Synthesize(context.Background(), input)
		requireSpeechError(test, err, "invalid_voice")
	}
}

func TestSelectedVoiceFailureNeverFallsBack(test *testing.T) {
	synthesisCalls := 0
	client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == microsoftSpeechBootstrap {
			return speechResponse(200, "application/json", `{"t":"test-token","r":"eastus"}`), nil
		}
		synthesisCalls++
		return speechResponse(400, "text/plain", "private voice unavailable upstream detail"), nil
	})
	service := New()
	service.synthesize = client.synthesize
	audio, err := service.Synthesize(context.Background(), Input{Text: "hello", Language: "en", Voice: "en-US-JennyNeural"})
	requireSpeechError(test, err, "upstream_error")
	if synthesisCalls != 1 || audio != nil || strings.Contains(err.Error(), "private") {
		test.Fatal("voice failure retried, fell back, or exposed upstream details")
	}
}

func TestVoiceCatalogCannotMutateAllowlist(test *testing.T) {
	catalog := Voices()
	catalog[0].ID = "injected"
	if _, err := validateInput(Input{Text: "hello", Language: "en", Voice: "injected"}); err == nil {
		test.Fatal("metadata consumer mutated synthesis allowlist")
	}
	if err := ValidateSettings(domain.DefaultSpeechSettings()); err != nil {
		test.Fatalf("metadata consumer changed default voice validation: %v", err)
	}
}
