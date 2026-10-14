package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type speechRoundTripper func(*http.Request) (*http.Response, error)

func (transport speechRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func speechResponse(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func mockSpeechClient(handler speechRoundTripper) *edgeClient {
	client := newEdgeClient()
	client.httpClient.Transport = handler
	return client
}

func requireSpeechError(test *testing.T, err error, code string) {
	test.Helper()
	var speechError *Error
	if !errors.As(err, &speechError) || speechError.Code != code {
		test.Fatalf("got error %v, want speech error %s", err, code)
	}
}

func TestBootstrapSignatureIndependentVector(test *testing.T) {
	// Generated independently using Node crypto HMAC and encodeURIComponent.
	want := "MSTranslatorAndroidApp::xt9lOZqx/mT7f3bWECRWHHB/l8CTWyZXK8nERit0rpY=::tue, 13 oct 2026 12:00:00 GMT::00112233445566778899aabbccddeeff"
	actual := signBootstrap(time.Date(2026, 10, 13, 14, 0, 0, 0, time.FixedZone("UTC+2", 7200)), "00112233445566778899aabbccddeeff")
	if actual != want {
		test.Fatalf("signature mismatch: %s", actual)
	}
}

func TestAuthorizationIsNotCachedAcrossRequests(test *testing.T) {
	bootstrapCalls := 0
	client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == microsoftSpeechBootstrap {
			bootstrapCalls++
			return speechResponse(200, "application/json", `{"t":"per-click-token","r":"eastus"}`), nil
		}
		return speechResponse(200, "audio/mpeg", "audio"), nil
	})
	for count := 0; count < 2; count++ {
		if _, err := client.synthesize(context.Background(), Input{Text: "hello"}, voice{name: "test", locale: "en-US"}); err != nil {
			test.Fatal(err)
		}
	}
	if bootstrapCalls != 2 {
		test.Fatal("authorization token retained across user requests")
	}
}

func TestTransportErrorRedactsSensitiveDetails(test *testing.T) {
	calls := 0
	client := mockSpeechClient(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private-token private-text https://sensitive.invalid")
	})
	audio, err := client.synthesize(context.Background(), Input{Text: "private-text"}, voice{name: "test", locale: "en-US"})
	requireSpeechError(test, err, "network_error")
	if calls != 1 || audio != nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "sensitive.invalid") {
		test.Fatal("transport error retried or exposed private details")
	}
}

func TestSynthesisProtocolEscapingAndChunkOrder(test *testing.T) {
	text := strings.Repeat("\u4e2d", 601) + ` <voice name="injected">& goodbye</voice>`
	var synthesizedText strings.Builder
	bootstrapCalls, synthesisCalls := 0, 0
	client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			test.Fatalf("unexpected method %s", request.Method)
		}
		if request.URL.String() == microsoftSpeechBootstrap {
			bootstrapCalls++
			if request.Header.Get("X-MT-Signature") == "" || request.Header.Get("User-Agent") != speechUserAgent {
				test.Fatal("missing bootstrap protocol headers")
			}
			return speechResponse(200, "application/json; charset=utf-8", `{"t":"opaque-token-exact","r":"eastus"}`), nil
		}
		synthesisCalls++
		if request.URL.String() != "https://eastus.tts.speech.microsoft.com/cognitiveservices/v1" || request.Header.Get("Authorization") != "opaque-token-exact" {
			test.Fatal("unexpected endpoint or modified Authorization")
		}
		if request.Header.Get("Content-Type") != "application/ssml+xml" || request.Header.Get("X-Microsoft-OutputFormat") != "audio-24khz-48kbitrate-mono-mp3" {
			test.Fatal("incorrect synthesis format")
		}
		var document struct {
			Voice []struct {
				Name    string `xml:"name,attr"`
				Prosody struct {
					Text     string     `xml:",chardata"`
					Children []struct{} `xml:",any"`
				} `xml:"prosody"`
			} `xml:"voice"`
		}
		if err := xml.NewDecoder(request.Body).Decode(&document); err != nil {
			test.Fatal(err)
		}
		if len(document.Voice) != 1 || len(document.Voice[0].Prosody.Children) != 0 || document.Voice[0].Name != "zh-CN-XiaoxiaoMultilingualNeural" {
			test.Fatal("SSML injection or incorrect voice")
		}
		chunk := document.Voice[0].Prosody.Text
		if len(chunk) > 1800 || !utf8.ValidString(chunk) {
			test.Fatal("chunk violates raw UTF-8 boundary")
		}
		synthesizedText.WriteString(chunk)
		return speechResponse(200, "audio/mpeg", strings.Repeat("audio", synthesisCalls)), nil
	})
	selectedVoice, err := validateInput(Input{Text: text, Language: "zh-CN"})
	if err != nil {
		test.Fatal(err)
	}
	audio, err := client.synthesize(context.Background(), Input{Text: text}, selectedVoice)
	if err != nil {
		test.Fatal(err)
	}
	if bootstrapCalls != 1 || synthesisCalls != 2 || synthesizedText.String() != text || string(audio) != "audioaudioaudio" {
		test.Fatalf("incorrect chunk reuse/order: bootstrap=%d synth=%d", bootstrapCalls, synthesisCalls)
	}
}

func TestBootstrapRejectsUnsafeOrMalformedPayload(test *testing.T) {
	for _, body := range []string{
		"", `{`, `null`, `{}`, `{"t":"secret","r":"eastus"} {}`,
		`{"t":"secret","r":"eastus"} trailing`,
	} {
		test.Run("malformed/"+body, func(test *testing.T) {
			client := mockSpeechClient(func(*http.Request) (*http.Response, error) { return speechResponse(200, "application/json", body), nil })
			_, _, err := client.authorize(context.Background())
			requireSpeechError(test, err, "invalid_response")
		})
	}
	for _, region := range []string{"", "127.0.0.1", "eastus.evil.example", "eastus/anything", "eastus@localhost", "eastus:443", "eastus\r\nHost: evil", "../eastus", strings.Repeat("a", 33)} {
		test.Run("region/"+region, func(test *testing.T) {
			body, _ := json.Marshal(map[string]string{"t": "sensitive-token", "r": region})
			calls := 0
			client := mockSpeechClient(func(*http.Request) (*http.Response, error) {
				calls++
				return speechResponse(200, "application/json", string(body)), nil
			})
			audio, err := client.synthesize(context.Background(), Input{Text: "hello"}, voice{name: "test", locale: "en-US"})
			requireSpeechError(test, err, "invalid_response")
			if audio != nil || calls != 1 || strings.Contains(err.Error(), "sensitive-token") {
				test.Fatal("unsafe authorization escaped validation")
			}
		})
	}
	for _, token := range []string{"", "  ", "secret\r\nInjected: yes", "secret\tvalue", "secret\x00", "secret\x7f", "secret\u4e2d", strings.Repeat("s", 16385)} {
		test.Run("token", func(test *testing.T) {
			body, _ := json.Marshal(map[string]string{"t": token, "r": "eastus"})
			client := mockSpeechClient(func(*http.Request) (*http.Response, error) {
				return speechResponse(200, "application/json", string(body)), nil
			})
			_, _, err := client.authorize(context.Background())
			requireSpeechError(test, err, "invalid_response")
		})
	}
}

func TestSpeechChunkBoundaries(test *testing.T) {
	for _, text := range []string{strings.Repeat("a", 3601), strings.Repeat("\u4e2d", 1201), strings.Repeat("\U0001f642", 901), strings.Repeat("word ", 800), strings.Repeat("&", 1801)} {
		chunks := splitSpeechText(text)
		if strings.Join(chunks, "") != text {
			test.Fatal("chunking lost or reordered text")
		}
		for _, chunk := range chunks {
			if len(chunk) > 1800 || !utf8.ValidString(chunk) {
				test.Fatal("invalid raw UTF-8 chunk")
			}
		}
	}
}

type truncatedSpeechBody struct{}

func (truncatedSpeechBody) Read(buffer []byte) (int, error) {
	return copy(buffer, "partial"), io.ErrUnexpectedEOF
}
func (truncatedSpeechBody) Close() error { return nil }

func TestRejectsInvalidAudio(test *testing.T) {
	for _, scenario := range []string{"wrong-type", "empty", "truncated", "oversize"} {
		test.Run(scenario, func(test *testing.T) {
			client := mockSpeechClient(func(*http.Request) (*http.Response, error) {
				response := speechResponse(200, "audio/mpeg", "audio")
				switch scenario {
				case "wrong-type":
					response.Header.Set("Content-Type", "text/html")
				case "empty":
					response.Body = io.NopCloser(strings.NewReader(""))
				case "truncated":
					response.Body = truncatedSpeechBody{}
				case "oversize":
					response.Body = io.NopCloser(bytes.NewReader(make([]byte, MaxAudioBytes+1)))
				}
				return response, nil
			})
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://unused.invalid", nil)
			audio, err := client.send(request, MaxAudioBytes, "audio/mpeg")
			requireSpeechError(test, err, "invalid_response")
			if audio != nil {
				test.Fatal("returned partial audio")
			}
		})
	}
}

func TestAggregateAudioLimitAndLaterFailure(test *testing.T) {
	for _, scenario := range []string{"exact-limit", "aggregate-overflow", "later-failure"} {
		test.Run(scenario, func(test *testing.T) {
			calls := 0
			client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == microsoftSpeechBootstrap {
					return speechResponse(200, "application/json", `{"t":"secret","r":"eastus"}`), nil
				}
				calls++
				if calls == 2 && scenario == "later-failure" {
					return speechResponse(500, "text/plain", "secret"), nil
				}
				length := MaxAudioBytes / 2
				if calls == 2 && scenario == "aggregate-overflow" {
					length++
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/mpeg"}}, Body: io.NopCloser(bytes.NewReader(make([]byte, length)))}, nil
			})
			audio, err := client.synthesize(context.Background(), Input{Text: strings.Repeat("a", 1801)}, voice{name: "test", locale: "en-US"})
			if scenario == "exact-limit" {
				if err != nil || len(audio) != MaxAudioBytes {
					test.Fatalf("exact limit failed: %v", err)
				}
			} else if err == nil || audio != nil {
				test.Fatal("failure returned partial audio")
			}
			if calls != 2 {
				test.Fatalf("got %d synthesis requests", calls)
			}
		})
	}
}

func TestUpstreamFailuresNeverRetryRedirectOrLeak(test *testing.T) {
	for _, stage := range []string{"bootstrap", "synthesis"} {
		for _, status := range []int{401, 403, 429, 302, 307, 500} {
			test.Run(stage+"/"+http.StatusText(status), func(test *testing.T) {
				failedCalls := 0
				client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
					if stage == "synthesis" && request.URL.String() == microsoftSpeechBootstrap {
						return speechResponse(200, "application/json", `{"t":"private-token","r":"eastus"}`), nil
					}
					failedCalls++
					response := speechResponse(status, "text/plain", "private-token private-text")
					response.Header.Set("Location", "https://attacker.invalid/private-token")
					return response, nil
				})
				audio, err := client.synthesize(context.Background(), Input{Text: "private-text"}, voice{name: "test", locale: "en-US"})
				code := "upstream_error"
				if status == 401 || status == 403 {
					code = "upstream_rejected"
				}
				if status == 429 {
					code = "rate_limited"
				}
				requireSpeechError(test, err, code)
				if failedCalls != 1 || audio != nil || strings.Contains(err.Error(), "private-") {
					test.Fatal("retry, redirect, partial audio, or sensitive error")
				}
			})
		}
	}
}

func TestHTTPBodyReadCancellationAndTimeout(test *testing.T) {
	for _, stage := range []string{"bootstrap", "synthesis"} {
		for _, timedOut := range []bool{false, true} {
			test.Run(stage+map[bool]string{false: "/cancel", true: "/timeout"}[timedOut], func(test *testing.T) {
				bodyStarted := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					_, _ = io.Copy(io.Discard, request.Body)
					if stage == "synthesis" && request.URL.Path == "/apps/endpoint" {
						writer.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(writer, `{"t":"secret","r":"eastus"}`)
						return
					}
					writer.Header().Set("Content-Type", map[bool]string{true: "application/json", false: "audio/mpeg"}[stage == "bootstrap"])
					writer.WriteHeader(200)
					writer.(http.Flusher).Flush()
					close(bodyStarted)
					<-request.Context().Done()
				}))
				defer server.Close()
				localURL, _ := url.Parse(server.URL)
				client := mockSpeechClient(func(request *http.Request) (*http.Response, error) {
					localRequest := request.Clone(request.Context())
					localRequest.URL.Scheme, localRequest.URL.Host = localURL.Scheme, localURL.Host
					return server.Client().Transport.RoundTrip(localRequest)
				})
				var ctx context.Context
				var cancel context.CancelFunc
				if timedOut {
					ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				result := make(chan error, 1)
				go func() {
					_, err := client.synthesize(ctx, Input{Text: "private-text"}, voice{name: "test", locale: "en-US"})
					result <- err
				}()
				select {
				case <-bodyStarted:
				case <-time.After(3 * time.Second):
					test.Fatal("body read never started")
				}
				if !timedOut {
					cancel()
				}
				select {
				case err := <-result:
					code := "cancelled"
					if timedOut {
						code = "timeout"
					}
					requireSpeechError(test, err, code)
				case <-time.After(3 * time.Second):
					test.Fatal("body read ignored context")
				}
			})
		}
	}
}
