package server

import (
	"context"
	"errors"
	"net/http"

	"cvtranslate/internal/update"
)

type UpdateChecker interface {
	Check(context.Context, string) (update.Result, error)
}

func (server *Server) checkForUpdate(writer http.ResponseWriter, request *http.Request) {
	if server.options.Updates == nil {
		writeError(writer, http.StatusServiceUnavailable, "update_check_unavailable", "Update check is unavailable in this session.")
		return
	}
	result, err := server.options.Updates.Check(request.Context(), server.options.Version)
	if err != nil {
		var checkError *update.Error
		if !errors.As(err, &checkError) {
			writeError(writer, http.StatusBadGateway, "update_check_failed", "Update check could not complete.")
			return
		}
		status := http.StatusBadGateway
		switch checkError.Code {
		case "rate_limited":
			status = http.StatusTooManyRequests
		case "timeout":
			status = http.StatusGatewayTimeout
		case "cancelled":
			status = http.StatusRequestTimeout
		}
		writeError(writer, status, checkError.Code, checkError.Message)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
