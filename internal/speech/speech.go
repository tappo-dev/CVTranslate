// Package speech implements bounded, on-demand online speech synthesis.
package speech

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"cvtranslate/internal/domain"
)

const MaxAudioBytes = 16 * 1024 * 1024
const synthesisTimeout = 120 * time.Second

type Input struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	Voice    string `json:"-"`
}

type Error struct {
	Code    string
	Message string
}

func (failure *Error) Error() string { return failure.Message }

func failure(code, message string) error {
	return &Error{Code: code, Message: message}
}

type voice struct {
	name   string
	locale string
}

func validateInput(input Input) (voice, error) {
	if !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > domain.MaxTextRunes {
		return voice{}, failure("invalid_input", "Read aloud requires between 1 and 20,000 characters.")
	}
	for _, character := range input.Text {
		if character < 0x20 && character != '\t' && character != '\n' && character != '\r' || character == 0xfffe || character == 0xffff {
			return voice{}, failure("invalid_input", "Read aloud text contains unsupported control characters.")
		}
	}
	language, err := ResolveLanguage(input.Text, input.Language)
	if err != nil {
		return voice{}, err
	}
	identifier := input.Voice
	if identifier == "" {
		defaults := domain.DefaultSpeechSettings().EdgeVoices
		identifier = defaults.English
		if language == "zh-CN" {
			identifier = defaults.Chinese
		}
	}
	selected, valid := findVoice(identifier, language)
	if !valid {
		return voice{}, failure("invalid_voice", "Choose an available Edge voice matching the read aloud language.")
	}
	return selected, nil
}

// ResolveLanguage applies the same bounded product language mapping for saved
// voice selection and synthesis; automatic selection uses Han script detection.
func ResolveLanguage(text, language string) (string, error) {
	if language == "auto" {
		language = domain.DetectSupportedLanguage(text)
	}
	switch language {
	case "en", "zh-CN":
		return language, nil
	default:
		return "", failure("invalid_language", "Read aloud supports English and Simplified Chinese only.")
	}
}

// Service never persists audio or text, retries requests, or selects a fallback.
// The shared slots bound memory and upstream work across all browser sessions.
type Service struct {
	slots      chan struct{}
	synthesize func(context.Context, Input, voice) ([]byte, error)
}

func New() *Service {
	client := newEdgeClient()
	return &Service{slots: make(chan struct{}, 2), synthesize: client.synthesize}
}

func (service *Service) Synthesize(ctx context.Context, input Input) ([]byte, error) {
	selectedVoice, err := validateInput(input)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, contextFailure(ctx)
	}
	select {
	case service.slots <- struct{}{}:
		defer func() { <-service.slots }()
	default:
		return nil, failure("speech_busy", "Read aloud is busy. Stop another request or try again later.")
	}
	requestContext, cancel := context.WithTimeout(ctx, synthesisTimeout)
	defer cancel()
	audio, err := service.synthesize(requestContext, input, selectedVoice)
	if requestContext.Err() != nil {
		return nil, contextFailure(requestContext)
	}
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 || len(audio) > MaxAudioBytes {
		return nil, failure("invalid_response", "Speech service returned empty or oversized audio.")
	}
	return audio, nil
}

func contextFailure(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return failure("timeout", "Speech synthesis timed out. Try a shorter text.")
	}
	return failure("cancelled", "Read aloud was cancelled.")
}
