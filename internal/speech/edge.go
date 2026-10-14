package speech

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"cvtranslate/internal/domain"
)

const microsoftSpeechBootstrap = "https://dev.microsofttranslator.com/apps/endpoint?api-version=1.0"
const microsoftSpeechClientID = "MSTranslatorAndroidApp"

// Published client signing material identifies the Microsoft consumer protocol.
// It is not a user's Azure subscription key and confers no availability guarantee.
const microsoftSpeechSigningKey = "oik6PdDdMnOXemTbwvMn9de/h9lFnfBaCWbGMMZqqoSaQaqUOqjVGm5NqsmjcBI1x+sS9ugjB55HEJWRiFXYFw=="
const speechUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36 Edg/127.0.0.0"
const maxSpeechChunkBytes = 1800

var microsoftRegionPattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

type edgeClient struct {
	httpClient *http.Client
	now        func() time.Time
}

func newEdgeClient() *edgeClient {
	return &edgeClient{
		httpClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		now: time.Now,
	}
}

func signBootstrap(now time.Time, nonce string) string {
	date := strings.ToLower(now.UTC().Format("Mon, 02 Jan 2006 15:04:05")) + " GMT"
	encodedEndpoint := url.QueryEscape(strings.TrimPrefix(microsoftSpeechBootstrap, "https://"))
	key, _ := base64.StdEncoding.DecodeString(microsoftSpeechSigningKey)
	signer := hmac.New(sha256.New, key)
	_, _ = signer.Write([]byte(strings.ToLower(microsoftSpeechClientID + encodedEndpoint + date + nonce)))
	digest := base64.StdEncoding.EncodeToString(signer.Sum(nil))
	return microsoftSpeechClientID + "::" + digest + "::" + date + "::" + nonce
}

func (client *edgeClient) authorize(ctx context.Context) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, microsoftSpeechBootstrap, nil)
	if err != nil {
		return "", "", failure("speech_failed", "Could not prepare the speech authorization request.")
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Accept-Language", "zh-Hans")
	request.Header.Set("X-ClientVersion", "4.0.530a 5fe1dc6c")
	request.Header.Set("X-UserId", "0f04d16a175c411e")
	request.Header.Set("X-HomeGeographicRegion", "zh-Hans-CN")
	request.Header.Set("X-ClientTraceId", domain.NewID())
	request.Header.Set("X-MT-Signature", signBootstrap(client.now(), domain.NewID()))
	request.Header.Set("User-Agent", speechUserAgent)
	body, err := client.send(request, 32*1024, "application/json")
	if err != nil {
		return "", "", err
	}
	var authorization struct {
		Token  string `json:"t"`
		Region string `json:"r"`
	}
	if json.Unmarshal(body, &authorization) != nil || !microsoftRegionPattern.MatchString(authorization.Region) ||
		strings.TrimSpace(authorization.Token) == "" || len(authorization.Token) > 16*1024 ||
		strings.ContainsFunc(authorization.Token, func(character rune) bool { return character < 0x20 || character > 0x7e }) {
		return "", "", failure("invalid_response", "Microsoft returned invalid speech authorization.")
	}
	// Only a region label can vary; upstream data cannot choose an arbitrary host.
	endpoint := "https://" + authorization.Region + ".tts.speech.microsoft.com/cognitiveservices/v1"
	return authorization.Token, endpoint, nil
}

func escapeSSML(text string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(text)) // bytes.Buffer writes cannot fail.
	return escaped.String()
}

func splitSpeechText(text string) []string {
	var chunks []string
	for len(text) > 0 {
		end := len(text)
		lastSpace := 0
		for offset, character := range text {
			if offset+utf8.RuneLen(character) > maxSpeechChunkBytes {
				end = offset
				if lastSpace > 0 {
					end = lastSpace
				}
				break
			}
			if unicode.IsSpace(character) {
				lastSpace = offset + utf8.RuneLen(character)
			}
		}
		if strings.TrimSpace(text[:end]) != "" {
			chunks = append(chunks, text[:end])
		}
		text = text[end:]
	}
	return chunks
}

func (client *edgeClient) synthesize(ctx context.Context, input Input, selectedVoice voice) ([]byte, error) {
	token, endpoint, err := client.authorize(ctx)
	if err != nil {
		return nil, err
	}
	var audio []byte
	for _, chunk := range splitSpeechText(input.Text) {
		ssml := `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="` + selectedVoice.locale + `"><voice name="` + selectedVoice.name + `"><prosody rate="+0%" pitch="+0Hz" volume="+0%">` + escapeSSML(chunk) + `</prosody></voice></speak>`
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(ssml))
		if err != nil {
			return nil, failure("speech_failed", "Could not prepare the speech synthesis request.")
		}
		request.Header.Set("Authorization", token)
		request.Header.Set("Content-Type", "application/ssml+xml")
		request.Header.Set("X-Microsoft-OutputFormat", "audio-24khz-48kbitrate-mono-mp3")
		request.Header.Set("User-Agent", speechUserAgent)
		chunkAudio, err := client.send(request, MaxAudioBytes-len(audio), "audio/mpeg")
		if err != nil {
			return nil, err // Never present a partial multi-chunk result as complete.
		}
		audio = append(audio, chunkAudio...)
	}
	return audio, nil
}

func (client *edgeClient) send(request *http.Request, limit int, expectedContentType string) ([]byte, error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, contextFailure(request.Context())
		}
		return nil, failure("network_error", "Could not connect to Microsoft read aloud. Check your network.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, failure("upstream_rejected", "Microsoft rejected read aloud. Check the system clock or try again later.")
		case http.StatusTooManyRequests:
			return nil, failure("rate_limited", "Microsoft read aloud is rate limited. Try again later.")
		default:
			return nil, failure("upstream_error", "Microsoft read aloud returned an unsuccessful response.")
		}
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != expectedContentType {
		return nil, failure("invalid_response", "Microsoft returned an unsupported speech response format.")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		if request.Context().Err() != nil {
			return nil, contextFailure(request.Context())
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, failure("invalid_response", "Microsoft returned incomplete speech data.")
		}
		return nil, failure("invalid_response", "Could not read the speech service response.")
	}
	if len(body) == 0 || len(body) > limit {
		return nil, failure("invalid_response", "Speech service returned empty or oversized data.")
	}
	return body, nil
}
