package speech

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVoiceMappingAndAutomaticSelection(test *testing.T) {
	for _, scenario := range []struct{ language, text, name, locale string }{
		{"en", "hello", "en-US-AndrewMultilingualNeural", "en-US"},
		{"zh-CN", "\u4e2d\u6587", "zh-CN-XiaoxiaoMultilingualNeural", "zh-CN"},
		{"auto", "hello", "en-US-AndrewMultilingualNeural", "en-US"},
		{"auto", "hello \u4e2d\u6587", "zh-CN-XiaoxiaoMultilingualNeural", "zh-CN"},
	} {
		test.Run(scenario.language+"/"+scenario.locale, func(test *testing.T) {
			selected, err := validateInput(Input{Text: scenario.text, Language: scenario.language})
			if err != nil || selected.name != scenario.name || selected.locale != scenario.locale {
				test.Fatalf("voice=%+v, error=%v", selected, err)
			}
		})
	}
}

func TestInputValidationBeforeNetwork(test *testing.T) {
	service := New()
	service.synthesize = func(context.Context, Input, voice) ([]byte, error) {
		test.Fatal("invalid input reached network")
		return nil, nil
	}
	for _, text := range []string{"", " \n\t", strings.Repeat("a", 20001), "\xff", "a\x00b", "a\u000bb", "\ufffe"} {
		_, err := service.Synthesize(context.Background(), Input{Text: text, Language: "en"})
		requireSpeechError(test, err, "invalid_input")
	}
	_, err := service.Synthesize(context.Background(), Input{Text: "hello", Language: "fr"})
	requireSpeechError(test, err, "invalid_language")
	if _, err := validateInput(Input{Text: strings.Repeat("\u4e2d", 20000), Language: "zh-CN"}); err != nil {
		test.Fatalf("rune boundary rejected: %v", err)
	}
}

func TestServiceConcurrencyDeadlineAndSlotRelease(test *testing.T) {
	service := New()
	entered := make(chan time.Duration, 3)
	release := make(chan struct{})
	defer close(release)
	service.synthesize = func(ctx context.Context, _ Input, _ voice) ([]byte, error) {
		deadline, exists := ctx.Deadline()
		if !exists {
			entered <- 0
		} else {
			entered <- time.Until(deadline)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return []byte("audio"), nil
		}
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	results := make(chan error, 2)
	for _, ctx := range []context.Context{firstContext, secondContext} {
		go func() { _, err := service.Synthesize(ctx, Input{Text: "hello", Language: "en"}); results <- err }()
	}
	for count := 0; count < 2; count++ {
		select {
		case remaining := <-entered:
			if remaining < 115*time.Second || remaining > 120*time.Second {
				test.Fatalf("unexpected service deadline %s", remaining)
			}
		case <-time.After(3 * time.Second):
			test.Fatal("concurrent request did not enter")
		}
	}
	_, err := service.Synthesize(context.Background(), Input{Text: "third", Language: "en"})
	requireSpeechError(test, err, "speech_busy")
	cancelFirst()
	select {
	case err := <-results:
		requireSpeechError(test, err, "cancelled")
	case <-time.After(3 * time.Second):
		test.Fatal("slot not released on cancellation")
	}
	replacementContext, cancelReplacement := context.WithCancel(context.Background())
	defer cancelReplacement()
	go func() {
		_, err := service.Synthesize(replacementContext, Input{Text: "replacement", Language: "en"})
		results <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		test.Fatal("replacement could not use released slot")
	}
	cancelSecond()
	cancelReplacement()
	for count := 0; count < 2; count++ {
		select {
		case err := <-results:
			requireSpeechError(test, err, "cancelled")
		case <-time.After(3 * time.Second):
			test.Fatal("request did not cancel")
		}
	}
}

func TestServiceRejectsEmptyOversizedAndAlreadyCancelled(test *testing.T) {
	service := New()
	calls := 0
	service.synthesize = func(context.Context, Input, voice) ([]byte, error) { calls++; return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Synthesize(ctx, Input{Text: "hello", Language: "en"})
	requireSpeechError(test, err, "cancelled")
	if calls != 0 {
		test.Fatal("already cancelled request started synthesis")
	}
	_, err = service.Synthesize(context.Background(), Input{Text: "hello", Language: "en"})
	requireSpeechError(test, err, "invalid_response")
	service.synthesize = func(context.Context, Input, voice) ([]byte, error) { return make([]byte, MaxAudioBytes+1), nil }
	audio, err := service.Synthesize(context.Background(), Input{Text: "hello", Language: "en"})
	requireSpeechError(test, err, "invalid_response")
	if audio != nil {
		test.Fatal("oversized result escaped service")
	}
}
