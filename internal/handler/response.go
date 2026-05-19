package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	reqID := middleware.GetReqID(r.Context())
	resp := map[string]string{"error": message}
	if reqID != "" {
		resp["request_id"] = reqID
	}
	writeJSON(w, status, resp)
}
