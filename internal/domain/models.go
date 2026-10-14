// Package domain contains contracts shared by translation entry points.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
	"unicode"
)

const MaxTextRunes = 20000

const BuiltInMicrosoftProviderID = "builtin-microsoft"
const DefaultOpenAIModel = "gpt-6-luna"

type ProviderConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	BaseURL        string `json:"baseUrl"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Enabled        bool   `json:"enabled"`
	HasAPIKey      bool   `json:"hasApiKey"`
}

type Prompt struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SystemTemplate string `json:"systemTemplate"`
	UserTemplate   string `json:"userTemplate"`
	Persona        string `json:"persona"`
	Revision       int    `json:"revision"`
	BuiltIn        bool   `json:"builtIn"`
}

type Hotkeys struct {
	OpenUI string `json:"openUi"`
}

type SpeechVoices struct {
	English string `json:"en"`
	Chinese string `json:"zh-CN"`
}

type SpeechSettings struct {
	Engine          string       `json:"engine"`
	EdgeVoices      SpeechVoices `json:"edgeVoices"`
	WebSpeechVoices SpeechVoices `json:"webSpeechVoices"`
}

type Settings struct {
	DefaultProviderID string         `json:"defaultProviderId"`
	DefaultPromptID   string         `json:"defaultPromptId"`
	SourceLanguage    string         `json:"sourceLanguage"`
	TargetLanguage    string         `json:"targetLanguage"`
	Port              int            `json:"port"`
	Hotkeys           Hotkeys        `json:"hotkeys"`
	Speech            SpeechSettings `json:"speech"`
}

type TranslationInput struct {
	Text           string `json:"text"`
	SourceLanguage string `json:"sourceLanguage"`
	TargetLanguage string `json:"targetLanguage"`
	ProviderID     string `json:"providerId"`
	PromptID       string `json:"promptId"`
	Entry          string `json:"entry"`
}

type Task struct {
	ID               string         `json:"id"`
	Text             string         `json:"text"`
	Translation      string         `json:"translation"`
	SourceLanguage   string         `json:"sourceLanguage"`
	TargetLanguage   string         `json:"targetLanguage"`
	Entry            string         `json:"entry"`
	Status           string         `json:"status"`
	ProviderSnapshot ProviderConfig `json:"providerSnapshot"`
	PromptSnapshot   Prompt         `json:"promptSnapshot"`
	ErrorCode        string         `json:"errorCode"`
	ErrorMessage     string         `json:"errorMessage"`
	PasteStatus      string         `json:"pasteStatus"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	DurationMS       int64          `json:"durationMs"`
	TokenUsage       *TokenUsage    `json:"tokenUsage,omitempty"`
	ReusedFromTaskID string         `json:"reusedFromTaskId,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
}

type TokenUsageSummary struct {
	RequestCount     int64 `json:"requestCount"`
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
}

type DailyTokenUsage struct {
	Date string `json:"date"`
	TokenUsageSummary
}

type TokenReport struct {
	Today      TokenUsageSummary `json:"today"`
	Last7Days  TokenUsageSummary `json:"last7Days"`
	Last30Days TokenUsageSummary `json:"last30Days"`
	Daily      []DailyTokenUsage `json:"daily"`
}

type TaskFilter struct {
	Page          int
	PageSize      int
	Query         string
	Status        string
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

type TaskPage struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var Languages = []Language{{Code: "en", Name: "English"}, {Code: "zh-CN", Name: "Simplified Chinese"}}

func LanguageName(code string) (string, bool) {
	if code == "auto" {
		return "automatically detected source language", true
	}
	for _, language := range Languages {
		if language.Code == code {
			return language.Name, true
		}
	}
	return "", false
}

// DetectSupportedLanguage applies the product's bounded English/Chinese
// heuristic: any Han character selects Chinese, otherwise English.
func DetectSupportedLanguage(text string) string {
	for _, character := range text {
		if unicode.Is(unicode.Han, character) {
			return "zh-CN"
		}
	}
	return "en"
}

func OppositeLanguage(code string) (string, bool) {
	switch code {
	case "en":
		return "zh-CN", true
	case "zh-CN":
		return "en", true
	default:
		return "", false
	}
}

func IsTerminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "interrupted"
}

func NewID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(bytes[:])
}

func DefaultSettings() Settings {
	return Settings{DefaultProviderID: BuiltInMicrosoftProviderID, DefaultPromptID: "builtin-general", SourceLanguage: "auto", TargetLanguage: "auto", Port: 19327, Speech: DefaultSpeechSettings()}
}

func DefaultSpeechSettings() SpeechSettings {
	return SpeechSettings{
		Engine: "edge",
		EdgeVoices: SpeechVoices{
			English: "en-US-AndrewMultilingualNeural",
			Chinese: "zh-CN-XiaoxiaoMultilingualNeural",
		},
	}
}
