package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
)

type errorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	status := apperr.HTTPStatus(err)
	response := errorResponse{}
	response.Error.Code = apperr.PublicCode(err)
	response.Error.Message = publicMessage(err)
	response.Error.RequestID = RequestID(r.Context())
	if status >= 500 {
		logger.Error("request failed", "request_id", response.Error.RequestID, "method", r.Method, "path", r.URL.Path, "error", err)
	} else {
		logger.Info("request rejected", "request_id", response.Error.RequestID, "method", r.Method, "path", r.URL.Path, "code", response.Error.Code)
	}
	writeJSON(w, status, response)
}

func publicMessage(err error) string {
	switch {
	case errors.Is(err, apperr.ErrUnauthenticated):
		return "authentication is required"
	case errors.Is(err, apperr.ErrForbidden):
		return "you cannot perform this action"
	case errors.Is(err, apperr.ErrNotFound):
		return "the requested resource was not found"
	case errors.Is(err, apperr.ErrConflict), errors.Is(err, apperr.ErrVersion):
		return "the resource conflicts with current state"
	case errors.Is(err, apperr.ErrExpired):
		return "the request or credential expired"
	case errors.Is(err, apperr.ErrCancelled):
		return "the operation was cancelled"
	case errors.Is(err, apperr.ErrInvalid):
		return "the request is invalid"
	default:
		return "an internal error occurred"
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return apperr.Wrap(apperr.ErrInvalid, "http.decode_json", "invalid JSON request", err)
	}
	return nil
}
