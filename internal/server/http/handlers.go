package http

import (
	"log/slog"
	"net/http"

	"github.com/kilnfi/cardano-validator-watcher/internal/watcher"
)

// Handler represents the HTTP handlers for the server
type Handler struct {
	logger      *slog.Logger
	healthStore *watcher.HealthStore
}

// NewHandler returns a new Handler
func NewHandler(logger *slog.Logger, healthStore *watcher.HealthStore) *Handler {
	return &Handler{
		logger:      logger,
		healthStore: healthStore,
	}
}

// Default redirects to the metrics endpoint
func (h *Handler) Default(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/metrics", http.StatusMovedPermanently)
		return
	}
	http.NotFound(w, r)
}

// Live checks the liveness of the service
// If the service is alive, it returns a 200 OK status
// If the service is not alive, it returns a 500 Internal Server Error status
func (h *Handler) LiveProbe(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Health OK"))
}

// Ready checks the readiness of the service by checking the health of our services
// If the service is ready, it returns a 200 OK status
// If the service is not ready, it returns a 500 Internal Server Error status
func (h *Handler) ReadyProbe(w http.ResponseWriter, _ *http.Request) {
	if !h.healthStore.GetHealth() {
		http.Error(w, "Health KO", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Health OK"))
}
