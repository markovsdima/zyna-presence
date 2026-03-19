package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/markovsdima/zyna-presence/internal/service"
)

const maxBatchSize = 200

type PresenceHandler struct {
	svc *service.PresenceService
}

func NewPresenceHandler(svc *service.PresenceService) *PresenceHandler {
	return &PresenceHandler{svc: svc}
}

// Heartbeat handles PUT /presence/{userID}.
func (h *PresenceHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	userID, _ := url.PathUnescape(chi.URLParam(r, "userID"))
	if userID == "" {
		http.Error(w, "missing user ID", http.StatusBadRequest)
		return
	}

	if err := h.svc.Heartbeat(r.Context(), userID); err != nil {
		slog.Error("heartbeat failed", "user_id", userID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type batchRequest struct {
	UserIDs []string `json:"user_ids"`
}

type batchResponse struct {
	Users map[string]service.UserStatus `json:"users"`
}

// BatchStatus handles POST /presence/status.
func (h *PresenceHandler) BatchStatus(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) == 0 {
		http.Error(w, "user_ids is required", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) > maxBatchSize {
		http.Error(w, "too many user_ids (max 200)", http.StatusBadRequest)
		return
	}

	statuses, err := h.svc.BatchStatus(r.Context(), req.UserIDs)
	if err != nil {
		slog.Error("batch status failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batchResponse{Users: statuses})
}

// Health handles GET /health.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
