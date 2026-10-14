package speech

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"cvtranslate/internal/domain"
)

type Voice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`
}

var builtInVoices = [...]Voice{
	{ID: "en-US-AndrewMultilingualNeural", Name: "Andrew Multilingual", Language: "en"},
	{ID: "en-US-AriaNeural", Name: "Aria", Language: "en"},
	{ID: "en-US-GuyNeural", Name: "Guy", Language: "en"},
	{ID: "en-US-JennyNeural", Name: "Jenny", Language: "en"},
	{ID: "zh-CN-XiaoxiaoMultilingualNeural", Name: "Xiaoxiao Multilingual", Language: "zh-CN"},
	{ID: "zh-CN-XiaoxiaoNeural", Name: "Xiaoxiao", Language: "zh-CN"},
	{ID: "zh-CN-XiaoyiNeural", Name: "Xiaoyi", Language: "zh-CN"},
	{ID: "zh-CN-YunxiNeural", Name: "Yunxi", Language: "zh-CN"},
}

// Voices returns local metadata without querying Microsoft or exposing a mutable
// reference to the allowlist used to validate synthesis requests.
func Voices() []Voice {
	return append([]Voice(nil), builtInVoices[:]...)
}

// NewPreviewInput permits only a catalog voice and a fixed language sample.
// Previewing a draft never needs to change the saved speech configuration.
func NewPreviewInput(language, identifier string) (Input, error) {
	var text string
	switch language {
	case "en":
		text = "Hello! This is a preview of the English reading voice."
	case "zh-CN":
		text = "\u4f60\u597d\uff01\u8fd9\u662f\u7b80\u4f53\u4e2d\u6587\u6717\u8bfb\u58f0\u97f3\u7684\u8bd5\u542c\u3002"
	default:
		return Input{}, failure("invalid_language", "Voice previews support English and Simplified Chinese only.")
	}
	if _, valid := findVoice(identifier, language); !valid {
		return Input{}, failure("invalid_voice", "Choose an available Edge voice matching the preview language.")
	}
	return Input{Text: text, Language: language, Voice: identifier}, nil
}

func findVoice(identifier, language string) (voice, bool) {
	for _, candidate := range builtInVoices {
		if candidate.ID != identifier || candidate.Language != language {
			continue
		}
		locale := "zh-CN"
		if language == "en" {
			locale = "en-US"
		}
		return voice{name: candidate.ID, locale: locale}, true
	}
	return voice{}, false
}

func ValidateSettings(settings domain.SpeechSettings) error {
	if settings.Engine != "edge" && settings.Engine != "web-speech" {
		return failure("invalid_speech_settings", "Choose Edge or Web Speech API for read aloud.")
	}
	if _, valid := findVoice(settings.EdgeVoices.English, "en"); !valid {
		return failure("invalid_speech_settings", "Choose an available English Edge voice.")
	}
	if _, valid := findVoice(settings.EdgeVoices.Chinese, "zh-CN"); !valid {
		return failure("invalid_speech_settings", "Choose an available Simplified Chinese Edge voice.")
	}
	for _, identifier := range []string{settings.WebSpeechVoices.English, settings.WebSpeechVoices.Chinese} {
		if len(identifier) > 1024 || !utf8.ValidString(identifier) || strings.ContainsFunc(identifier, unicode.IsControl) {
			return failure("invalid_speech_settings", "Browser voice identifiers must be valid text of at most 1,024 bytes without control characters.")
		}
	}
	return nil
}
