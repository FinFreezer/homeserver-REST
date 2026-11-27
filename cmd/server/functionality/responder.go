package functionality

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

func respondWithError(w http.ResponseWriter, code int, msg string, err error, logger *slog.Logger) {
	if err != nil {
		logger.Error("responding with an error",
			slog.String("error", fmt.Sprintf("%+v", err)),
		)
	}
	if code > 499 {
		logger.Error(fmt.Sprintf("responding with a 5xx error: %s", msg))
	}
	type errorResponse struct {
		Error string `json:"error"`
	}
	respondWithJSON(w, code, errorResponse{
		Error: msg,
	}, logger)
}

func respondWithJSON(w http.ResponseWriter, code int, payload any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	dat, err := json.Marshal(payload)
	if err != nil {
		logger.Error(fmt.Sprintf("Error marshaling JSON: %s", err),
			slog.String("component", "respondwithjson"))
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(dat)
	logger.Error("problem writing to upstream",
		slog.String("component", "respondwithjson"),
		slog.String("error", fmt.Sprintf("%+v", err)))
}
