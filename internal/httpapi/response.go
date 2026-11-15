package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"ledger/internal/ledger"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

// writeError translates domain errors into status codes; anything else is a 500
// that never leaks internal details to the client.
func writeError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "internal error"
	switch {
	case errors.Is(err, ledger.ErrInvalidInput):
		status, message = http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, ledger.ErrNotFound):
		status, message = http.StatusNotFound, "not found"
	case errors.Is(err, ledger.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	default:
		slog.Error("request failed", "err", err)
	}
	writeJSON(w, status, map[string]string{"error": message})
}
