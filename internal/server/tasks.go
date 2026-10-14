package server

import (
	"net/http"
	"strconv"
	"time"

	"cvtranslate/internal/domain"
)

func (server *Server) startTask(writer http.ResponseWriter, request *http.Request) {
	var input domain.TranslationInput
	if !readJSON(writer, request, &input) {
		return
	}
	input.Entry = "web"
	task, err := server.StartInput(input)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "translation_rejected", err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, task)
}

func (server *Server) getTask(writer http.ResponseWriter, request *http.Request) {
	task, err := server.store.GetTask(request.Context(), request.PathValue("id"))
	if err != nil {
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, task)
}

func (server *Server) cancelTask(writer http.ResponseWriter, request *http.Request) {
	task, err := server.jobs.Cancel(request.PathValue("id"))
	if err != nil {
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, task)
}

func (server *Server) listHistory(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if page > 100000 || pageSize > 100 || len(query.Get("q")) > 500 {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "History filter is too large.")
		return
	}
	createdAfter, err := parseHistoryTimestamp(query.Get("createdAfter"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "History start date is invalid.")
		return
	}
	createdBefore, err := parseHistoryTimestamp(query.Get("createdBefore"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "History end date is invalid.")
		return
	}
	if !createdAfter.IsZero() && !createdBefore.IsZero() && !createdAfter.Before(createdBefore) {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "History date range is invalid.")
		return
	}
	result, err := server.store.ListTasks(request.Context(), domain.TaskFilter{
		Page:          page,
		PageSize:      pageSize,
		Query:         query.Get("q"),
		Status:        query.Get("status"),
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
	})
	if err != nil {
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func parseHistoryTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func (server *Server) getTokenReport(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	days := 365
	if daysValue := query.Get("days"); daysValue != "" {
		parsedDays, err := strconv.Atoi(daysValue)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid_filter", "Token report range is invalid.")
			return
		}
		days = parsedDays
	}
	timezoneOffsetMinutes, err := strconv.Atoi(query.Get("timezoneOffsetMinutes"))
	if err != nil && query.Get("timezoneOffsetMinutes") != "" {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "Token report timezone is invalid.")
		return
	}
	if days < 30 || days > 366 || timezoneOffsetMinutes < -14*60 || timezoneOffsetMinutes > 14*60 {
		writeError(writer, http.StatusBadRequest, "invalid_filter", "Token report range is invalid.")
		return
	}
	report, err := server.store.GetTokenReport(request.Context(), days, timezoneOffsetMinutes)
	if err != nil {
		storeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, report)
}

func (server *Server) deleteHistory(writer http.ResponseWriter, request *http.Request) {
	if err := server.store.DeleteTask(request.Context(), request.PathValue("id")); err != nil {
		storeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) clearHistory(writer http.ResponseWriter, request *http.Request) {
	if err := server.store.ClearTasks(request.Context()); err != nil {
		storeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
