package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	"cvtranslate/internal/provider"
	"cvtranslate/internal/speech"
)

func (server *Server) getState(writer http.ResponseWriter, request *http.Request) {
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	providers, err := server.store.ListProviders(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	for index := range providers {
		capabilities, _ := provider.CapabilitiesFor(providers[index].Type)
		providers[index].HasAPIKey = false
		if !capabilities.SupportsAPIKey {
			continue
		}
		if server.options.Credentials == nil {
			credentialError(writer)
			return
		}
		providers[index].HasAPIKey, err = server.options.Credentials.Has(providers[index].ID)
		if err != nil {
			credentialError(writer)
			return
		}
	}
	prompts, err := server.store.ListPrompts(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	settings, err := server.store.GetSettings(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	var status any = map[string]any{"os": "unknown", "hotkeysAvailable": false, "pasteAvailable": false, "message": "Native integration disabled."}
	if server.options.PlatformStatus != nil {
		status = server.options.PlatformStatus()
	}
	writeJSON(writer, http.StatusOK, map[string]any{"providers": providers, "providerTypes": provider.Registry(), "prompts": prompts, "settings": settings, "languages": domain.Languages, "speechVoices": speech.Voices(), "platform": status, "version": server.options.Version, "development": server.options.Development})
}

type providerSave struct {
	domain.ProviderConfig
	APIKey                *string `json:"apiKey"`
	ClearAPIKey           bool    `json:"clearApiKey"`
	ConfirmEndpointChange bool    `json:"confirmEndpointChange"`
}

func (server *Server) saveProvider(writer http.ResponseWriter, request *http.Request) {
	var input providerSave
	if !readJSON(writer, request, &input) {
		return
	}
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	if input.Type == "" {
		input.Type = "openai-compatible"
	}
	capabilities, capabilitiesExist := provider.CapabilitiesFor(input.Type)
	if capabilitiesExist && input.ID == "" && capabilities.RequiresModel && input.Model == "" {
		input.Model = capabilities.DefaultModel
	}
	if err := provider.ValidateConfig(input.ProviderConfig); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_provider", err.Error())
		return
	}
	if input.APIKey != nil && (len(*input.APIKey) > 8192 || strings.ContainsAny(*input.APIKey, "\r\n")) {
		writeError(writer, http.StatusBadRequest, "invalid_key", "Invalid API key.")
		return
	}
	if input.APIKey != nil && input.ClearAPIKey {
		writeError(writer, http.StatusBadRequest, "invalid_key", "Choose replacing or clearing the key, not both.")
		return
	}
	capabilities, _ = provider.CapabilitiesFor(input.Type)
	if !capabilities.SupportsAPIKey && (input.ClearAPIKey || input.APIKey != nil && *input.APIKey != "") {
		writeError(writer, http.StatusBadRequest, "invalid_key", "This provider does not accept API keys.")
		return
	}
	existing := input.ID != ""
	var previous domain.ProviderConfig
	var err error
	if existing {
		previous, err = server.store.GetProvider(request.Context(), input.ID)
		if err != nil {
			storeError(writer, err)
			return
		}
		if previous.Type != input.Type {
			writeError(writer, http.StatusConflict, "provider_type_immutable", "Create a new provider to use a different provider type.")
			return
		}
	} else {
		input.ID = domain.NewID()
	}
	settings, err := server.store.GetSettings(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	if !input.Enabled && settings.DefaultProviderID == input.ID {
		writeError(writer, http.StatusConflict, "default_in_use", "Choose a different default before disabling this provider.")
		return
	}
	var oldKey string
	if capabilities.SupportsAPIKey {
		if server.options.Credentials == nil {
			credentialError(writer)
			return
		}
		oldKey, err = server.options.Credentials.Get(input.ID)
		if err != nil {
			credentialError(writer)
			return
		}
	}
	endpointChanged := existing && previous.BaseURL != input.BaseURL
	if endpointChanged && oldKey != "" && input.APIKey == nil && !input.ClearAPIKey && !input.ConfirmEndpointChange {
		writeError(writer, http.StatusConflict, "endpoint_confirmation", "Confirm using the existing key at the changed endpoint, replace it, or clear it.")
		return
	}
	newKey := oldKey
	if input.APIKey != nil {
		newKey = *input.APIKey
	}
	if input.ClearAPIKey {
		newKey = ""
	}
	// Remove credentials before changing an endpoint. An interrupted update must
	// fail closed instead of silently forwarding an old credential to a new host.
	if endpointChanged && oldKey != "" {
		if err := server.options.Credentials.Delete(input.ID); err != nil {
			credentialError(writer)
			return
		}
	}
	input.HasAPIKey = false // Derived on responses, not a persisted secret indicator.
	if err := server.store.SaveProvider(request.Context(), input.ProviderConfig); err != nil {
		if endpointChanged && oldKey != "" {
			_ = server.options.Credentials.Set(input.ID, oldKey)
		}
		storeError(writer, err)
		return
	}
	if capabilities.SupportsAPIKey && (endpointChanged || input.APIKey != nil || input.ClearAPIKey) {
		if newKey == "" {
			err = server.options.Credentials.Delete(input.ID)
		} else {
			err = server.options.Credentials.Set(input.ID, newKey)
		}
		if err != nil {
			recoveryContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var recoveryError error
			if existing {
				recoveryError = server.store.SaveProvider(recoveryContext, previous)
			} else {
				recoveryError = server.store.DeleteProvider(recoveryContext, input.ID)
			}
			// Only restore the old key when its original endpoint was restored.
			if recoveryError == nil && oldKey != "" {
				_ = server.options.Credentials.Set(input.ID, oldKey)
			}
			credentialError(writer)
			return
		}
	}
	input.HasAPIKey = newKey != ""
	writeJSON(writer, http.StatusOK, input.ProviderConfig)
}

func (server *Server) deleteProvider(writer http.ResponseWriter, request *http.Request) {
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	identifier := request.PathValue("id")
	settings, err := server.store.GetSettings(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	if settings.DefaultProviderID == identifier {
		writeError(writer, http.StatusConflict, "default_in_use", "Change the default provider before deleting it.")
		return
	}
	configuration, err := server.store.GetProvider(request.Context(), identifier)
	if err != nil {
		storeError(writer, err)
		return
	}
	capabilities, _ := provider.CapabilitiesFor(configuration.Type)
	var oldKey string
	if capabilities.SupportsAPIKey {
		if server.options.Credentials == nil {
			credentialError(writer)
			return
		}
		oldKey, err = server.options.Credentials.Get(identifier)
		if err != nil {
			credentialError(writer)
			return
		}
		if err := server.options.Credentials.Delete(identifier); err != nil {
			credentialError(writer)
			return
		}
	}
	if err := server.store.DeleteProvider(request.Context(), identifier); err != nil {
		if oldKey != "" {
			_ = server.options.Credentials.Set(identifier, oldKey)
		}
		storeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) testProvider(writer http.ResponseWriter, request *http.Request) {
	server.configurationMutex.Lock()
	configuration, err := server.store.GetProvider(request.Context(), request.PathValue("id"))
	if err != nil {
		server.configurationMutex.Unlock()
		storeError(writer, err)
		return
	}
	capabilities, _ := provider.CapabilitiesFor(configuration.Type)
	var apiKey string
	if capabilities.SupportsAPIKey {
		if server.options.Credentials == nil {
			server.configurationMutex.Unlock()
			credentialError(writer)
			return
		}
		apiKey, err = server.options.Credentials.Get(configuration.ID)
	}
	server.configurationMutex.Unlock()
	if err != nil {
		credentialError(writer)
		return
	}
	_, err = provider.Translate(request.Context(), configuration, apiKey, provider.TranslationRequest{Text: "Hello.", SourceLanguage: "auto", TargetLanguage: "en", SystemPrompt: "Translate the user's text into English. Return only the translation.", UserPrompt: "Hello."})
	if err != nil {
		writeError(writer, http.StatusBadGateway, "provider_test_failed", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) savePrompt(writer http.ResponseWriter, request *http.Request) {
	var input domain.Prompt
	if !readJSON(writer, request, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 160 {
		writeError(writer, http.StatusBadRequest, "invalid_prompt", "A prompt name of up to 160 bytes is required.")
		return
	}
	if err := prompt.Validate(input.SystemTemplate, input.UserTemplate); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_prompt", err.Error())
		return
	}
	if err := prompt.ValidatePersona(input.Persona); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_prompt", err.Error())
		return
	}
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	if input.ID != "" {
		previous, err := server.store.GetPrompt(request.Context(), input.ID)
		if err != nil {
			storeError(writer, err)
			return
		}
		if previous.BuiltIn {
			writeError(writer, http.StatusConflict, "builtin_readonly", "Copy the built-in prompt before editing.")
			return
		}
		if input.Revision != previous.Revision {
			writeError(writer, http.StatusConflict, "stale_revision", "This prompt changed. Reload before saving.")
			return
		}
		input.Revision = previous.Revision + 1
	} else {
		input.ID = domain.NewID()
		input.Revision = 1
	}
	input.BuiltIn = false
	if err := server.store.SavePrompt(request.Context(), input); err != nil {
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, input)
}

func (server *Server) deletePrompt(writer http.ResponseWriter, request *http.Request) {
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	identifier := request.PathValue("id")
	configuration, err := server.store.GetPrompt(request.Context(), identifier)
	if err != nil {
		storeError(writer, err)
		return
	}
	settings, err := server.store.GetSettings(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	if configuration.BuiltIn || settings.DefaultPromptID == identifier {
		writeError(writer, http.StatusConflict, "prompt_in_use", "Built-in and default prompts cannot be deleted.")
		return
	}
	if err := server.store.DeletePrompt(request.Context(), identifier); err != nil {
		storeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) previewPrompt(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		SystemTemplate string `json:"systemTemplate"`
		UserTemplate   string `json:"userTemplate"`
		Persona        string `json:"persona"`
		Text           string `json:"text"`
		SourceLanguage string `json:"sourceLanguage"`
		TargetLanguage string `json:"targetLanguage"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	systemText, userText, err := prompt.Render(domain.Prompt{SystemTemplate: input.SystemTemplate, UserTemplate: input.UserTemplate, Persona: input.Persona}, input.Text, input.SourceLanguage, input.TargetLanguage)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_prompt", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"system": systemText, "user": userText})
}

func (server *Server) saveSettings(writer http.ResponseWriter, request *http.Request) {
	var input domain.Settings
	if !readJSON(writer, request, &input) {
		return
	}
	if _, valid := domain.LanguageName(input.SourceLanguage); !valid {
		writeError(writer, http.StatusBadRequest, "invalid_language", "Unknown source language.")
		return
	}
	if _, valid := domain.LanguageName(input.TargetLanguage); !valid {
		writeError(writer, http.StatusBadRequest, "invalid_language", "Choose a target language.")
		return
	}
	if input.Port < 1024 || input.Port > 65535 {
		writeError(writer, http.StatusBadRequest, "invalid_port", "Use a port between 1024 and 65535.")
		return
	}
	if err := speech.ValidateSettings(input.Speech); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_speech_settings", err.Error())
		return
	}
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	if input.DefaultProviderID != "" {
		configuration, err := server.store.GetProvider(request.Context(), input.DefaultProviderID)
		if err != nil {
			storeError(writer, err)
			return
		}
		if !configuration.Enabled {
			writeError(writer, http.StatusBadRequest, "provider_disabled", "The default provider must be enabled.")
			return
		}
	}
	if _, err := server.store.GetPrompt(request.Context(), input.DefaultPromptID); err != nil {
		storeError(writer, err)
		return
	}
	previous, err := server.store.GetSettings(request.Context())
	if err != nil {
		storeError(writer, err)
		return
	}
	changed := input.Hotkeys != previous.Hotkeys
	if changed {
		if server.options.ConfigureHotkeys == nil {
			writeError(writer, http.StatusServiceUnavailable, "native_disabled", "Global shortcuts are unavailable in this session.")
			return
		}
		if err := server.options.ConfigureHotkeys(input.Hotkeys); err != nil {
			writeError(writer, http.StatusConflict, "hotkey_failed", err.Error())
			return
		}
	}
	if err := server.store.SaveSettings(request.Context(), input); err != nil {
		if changed {
			if rollbackError := server.options.ConfigureHotkeys(previous.Hotkeys); rollbackError != nil {
				writeError(writer, http.StatusInternalServerError, "hotkey_rollback_failed", "Settings were not saved and shortcut restoration failed. Restart the application.")
				return
			}
		}
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, input)
}

func credentialError(writer http.ResponseWriter) {
	writeError(writer, http.StatusServiceUnavailable, "credential_store_failed", "The operating system credential store is unavailable. No plaintext fallback is used.")
}
