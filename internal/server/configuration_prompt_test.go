package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
)

func TestPromptPersonaSavePreviewAndRevision(test *testing.T) {
	application := newIntegrationServer(test)
	template := prompt.BuiltIn()
	template.ID = ""
	template.BuiltIn = false
	template.Persona = "Optimistic and kind; keep {{text}} literal."
	response := performRequest(application, "POST", "/api/prompts", jsonText(test, template), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("save failed: %d %s", response.Code, response.Body.String())
	}
	var saved domain.Prompt
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		test.Fatal(err)
	}
	stored, err := application.store.GetPrompt(context.Background(), saved.ID)
	if err != nil || stored != saved || stored.Persona != template.Persona || stored.Revision != 1 {
		test.Fatalf("persona did not round-trip: %+v %v", stored, err)
	}
	previewInput := map[string]string{
		"systemTemplate": saved.SystemTemplate, "userTemplate": saved.UserTemplate,
		"persona": saved.Persona, "text": "Please fix this bug.",
		"sourceLanguage": "en", "targetLanguage": "zh-CN",
	}
	response = performRequest(application, "POST", "/api/prompts/preview", jsonText(test, previewInput), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("preview failed: %d %s", response.Code, response.Body.String())
	}
	var preview map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		test.Fatal(err)
	}
	expectedSystem, expectedUser, err := prompt.Render(saved, previewInput["text"], "en", "zh-CN")
	if err != nil || preview["system"] != expectedSystem || preview["user"] != expectedUser || !strings.Contains(preview["system"], saved.Persona) {
		test.Fatal("preview did not use the shared persona renderer")
	}
	history, err := application.store.ListTasks(context.Background(), domain.TaskFilter{})
	if err != nil || history.Total != 0 {
		test.Fatal("local preview created a translation task")
	}
	saved.Persona = ""
	response = performRequest(application, "POST", "/api/prompts", jsonText(test, saved), testOrigin)
	if response.Code != http.StatusOK {
		test.Fatalf("clearing persona failed: %d", response.Code)
	}
	stored, err = application.store.GetPrompt(context.Background(), saved.ID)
	if err != nil || stored.Persona != "" || stored.Revision != 2 {
		test.Fatal("clearing persona did not create a new revision")
	}
	response = performRequest(application, "POST", "/api/prompts", jsonText(test, saved), testOrigin)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "stale_revision") {
		test.Fatal("stale persona edit overwrote the saved revision")
	}
}

func TestOversizedPersonaRejectedBySaveAndPreview(test *testing.T) {
	application := newIntegrationServer(test)
	template := prompt.BuiltIn()
	template.ID = ""
	template.BuiltIn = false
	template.Persona = strings.Repeat("x", prompt.MaxPersonaRunes+1)
	response := performRequest(application, "POST", "/api/prompts", jsonText(test, template), testOrigin)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_prompt") {
		test.Fatal("save accepted an oversized persona")
	}
	response = performRequest(application, "POST", "/api/prompts/preview", jsonText(test, map[string]string{
		"systemTemplate": template.SystemTemplate, "userTemplate": template.UserTemplate,
		"persona": template.Persona, "text": "source", "sourceLanguage": "en", "targetLanguage": "zh-CN",
	}), testOrigin)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_prompt") {
		test.Fatal("preview accepted an oversized persona")
	}
	prompts, err := application.store.ListPrompts(context.Background())
	if err != nil || len(prompts) != 1 || prompts[0] != prompt.BuiltIn() {
		test.Fatal("invalid persona changed saved prompts")
	}
}
