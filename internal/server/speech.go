package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"cvtranslate/internal/speech"
)

type SpeechSynthesizer interface {
	Synthesize(context.Context, speech.Input) ([]byte, error)
}

func (server *Server) synthesizeSpeech(writer http.ResponseWriter, request *http.Request) {
	var input speech.Input
	if !readJSON(writer, request, &input) {
		return
	}
	server.configurationMutex.Lock()
	settings, err := server.store.GetSettings(request.Context())
	server.configurationMutex.Unlock()
	if err != nil {
		storeError(writer, err)
		return
	}
	if err := speech.ValidateSettings(settings.Speech); err != nil {
		writeError(writer, http.StatusConflict, "invalid_speech_settings", "Saved read aloud settings are invalid. Choose available voices in settings and save again.")
		return
	}
	if settings.Speech.Engine != "edge" {
		writeError(writer, http.StatusConflict, "speech_engine_mismatch", "Read aloud is configured to use Web Speech API. Refresh the page or choose Edge in settings.")
		return
	}
	language, err := speech.ResolveLanguage(input.Text, input.Language)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_language", err.Error())
		return
	}
	input.Voice = settings.Speech.EdgeVoices.English
	if language == "zh-CN" {
		input.Voice = settings.Speech.EdgeVoices.Chinese
	}
	server.respondWithSpeech(writer, request, input)
}

func (server *Server) previewSpeech(writer http.ResponseWriter, request *http.Request) {
	var preview struct {
		Language string `json:"language"`
		VoiceID  string `json:"voiceId"`
	}
	if !readJSON(writer, request, &preview) {
		return
	}
	input, err := speech.NewPreviewInput(preview.Language, preview.VoiceID)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_speech_preview", err.Error())
		return
	}
	server.respondWithSpeech(writer, request, input)
}

func (server *Server) respondWithSpeech(writer http.ResponseWriter, request *http.Request, input speech.Input) {
	if server.options.Speech == nil {
		writeError(writer, http.StatusServiceUnavailable, "speech_unavailable", "Read aloud is unavailable in this session.")
		return
	}
	audio, err := server.options.Speech.Synthesize(request.Context(), input)
	if err != nil {
		var synthesisError *speech.Error
		if !errors.As(err, &synthesisError) {
			writeError(writer, http.StatusBadGateway, "speech_failed", "Speech synthesis could not complete.")
			return
		}
		status := http.StatusBadGateway
		switch synthesisError.Code {
		case "invalid_input", "invalid_language", "invalid_voice":
			status = http.StatusBadRequest
		case "speech_busy", "rate_limited":
			status = http.StatusTooManyRequests
		case "timeout":
			status = http.StatusGatewayTimeout
		case "cancelled":
			status = http.StatusRequestTimeout
		}
		writeError(writer, status, synthesisError.Code, synthesisError.Message)
		return
	}
	if len(audio) == 0 || len(audio) > speech.MaxAudioBytes {
		writeError(writer, http.StatusBadGateway, "invalid_response", "Speech service returned empty or oversized audio.")
		return
	}
	writer.Header().Set("Content-Type", "audio/mpeg")
	writer.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(audio)
}
