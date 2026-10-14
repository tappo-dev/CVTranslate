// Package server exposes the origin-checked loopback management API.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/storage"
	"cvtranslate/internal/translation"
)

type Credentials interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
	Has(string) (bool, error)
}

type Options struct {
	Origin           string
	Version          string
	Development      bool
	Assets           http.Handler
	Credentials      Credentials
	Speech           SpeechSynthesizer
	Updates          UpdateChecker
	PlatformStatus   func() any
	ConfigureHotkeys func(domain.Hotkeys) error
	OpenBrowser      func(string) error
	ControlToken     string
}

type Server struct {
	store              *storage.Store
	jobs               *translation.Service
	options            Options
	router             *http.ServeMux
	configurationMutex sync.Mutex
}

func New(store *storage.Store, jobs *translation.Service, options Options) *Server {
	server := &Server{store: store, jobs: jobs, options: options, router: http.NewServeMux()}
	server.router.HandleFunc("GET /api/state", server.getState)
	server.router.HandleFunc("POST /api/providers", server.saveProvider)
	server.router.HandleFunc("DELETE /api/providers/{id}", server.deleteProvider)
	server.router.HandleFunc("POST /api/providers/{id}/test", server.testProvider)
	server.router.HandleFunc("POST /api/prompts", server.savePrompt)
	server.router.HandleFunc("POST /api/prompts/preview", server.previewPrompt)
	server.router.HandleFunc("DELETE /api/prompts/{id}", server.deletePrompt)
	server.router.HandleFunc("PUT /api/settings", server.saveSettings)
	server.router.HandleFunc("POST /api/tasks", server.startTask)
	server.router.HandleFunc("GET /api/tasks/{id}", server.getTask)
	server.router.HandleFunc("POST /api/tasks/{id}/cancel", server.cancelTask)
	server.router.HandleFunc("POST /api/speech", server.synthesizeSpeech)
	server.router.HandleFunc("POST /api/speech/preview", server.previewSpeech)
	server.router.HandleFunc("POST /api/update-check", server.checkForUpdate)
	server.router.HandleFunc("GET /api/history", server.listHistory)
	server.router.HandleFunc("GET /api/token-report", server.getTokenReport)
	server.router.HandleFunc("DELETE /api/history/{id}", server.deleteHistory)
	server.router.HandleFunc("DELETE /api/history", server.clearHistory)
	server.router.HandleFunc("/api/", func(writer http.ResponseWriter, _ *http.Request) {
		writeError(writer, http.StatusNotFound, "not_found", "API endpoint not found.")
	})
	return server
}

func (server *Server) BrowserURL(taskID string) string {
	address := server.options.Origin + "/"
	if taskID == "" {
		return address
	}
	return address + "?" + url.Values{"task": []string{taskID}}.Encode()
}

func (server *Server) OpenUI(taskID string) error {
	if server.options.OpenBrowser == nil {
		return errors.New("browser opening unavailable")
	}
	return server.options.OpenBrowser(server.BrowserURL(taskID))
}

// StartInput serializes config capture with edits, for both HTTP and native jobs.
func (server *Server) StartInput(input domain.TranslationInput) (domain.Task, error) {
	server.configurationMutex.Lock()
	defer server.configurationMutex.Unlock()
	return server.jobs.Start(input)
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	expectedHost := strings.TrimPrefix(server.options.Origin, "http://")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Cache-Control", "no-store")
	scriptPolicy := "script-src 'self'; "
	connectionPolicy := "connect-src 'self'; "
	if server.options.Development {
		scriptPolicy = "script-src 'self' 'unsafe-inline'; "
		connectionPolicy = "connect-src 'self' ws://" + expectedHost + "; "
	}
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; "+scriptPolicy+connectionPolicy+"style-src 'self' 'unsafe-inline'; img-src 'self' data:; media-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	fetchSite := request.Header.Get("Sec-Fetch-Site")
	if request.Host != expectedHost || (fetchSite != "" && fetchSite != "none" && fetchSite != "same-origin") {
		writeError(writer, http.StatusForbidden, "invalid_host", "Only the local application origin is allowed.")
		return
	}
	origin := request.Header.Get("Origin")
	if origin != "" && origin != server.options.Origin {
		writeError(writer, http.StatusForbidden, "invalid_origin", "Request origin was rejected.")
		return
	}
	if request.URL.Path == "/healthz" && request.Method == http.MethodGet {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if request.URL.Path == "/internal/open" {
		if request.Method != http.MethodPost || server.options.ControlToken == "" || subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Instance-Token")), []byte(server.options.ControlToken)) != 1 {
			writeError(writer, http.StatusForbidden, "invalid_control", "Local instance authorization required.")
			return
		}
		if err := server.OpenUI(""); err != nil {
			writeError(writer, http.StatusServiceUnavailable, "browser_failed", "Could not open the browser.")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/") {
		mutation := request.Method != http.MethodGet && request.Method != http.MethodHead
		if mutation && origin != server.options.Origin {
			writeError(writer, http.StatusForbidden, "invalid_origin", "An exact local Origin header is required.")
			return
		}
		server.router.ServeHTTP(writer, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if server.options.Assets == nil {
		http.NotFound(writer, request)
		return
	}
	server.options.Assets.ServeHTTP(writer, request)
}

func readJSON(writer http.ResponseWriter, request *http.Request, value any) bool {
	if strings.Split(request.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "invalid_content_type", "Use application/json.")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 256*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Invalid or oversized JSON request.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Only one JSON value is accepted.")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func storeError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", "The record no longer exists.")
	case errors.Is(err, storage.ErrConflict):
		writeError(writer, http.StatusConflict, "conflict", "The record is still in use or a task is active.")
	case errors.Is(err, context.Canceled):
		writeError(writer, http.StatusRequestTimeout, "cancelled", "The operation was cancelled.")
	default:
		writeError(writer, http.StatusInternalServerError, "storage_failed", "Local storage operation failed.")
	}
}
