package api

import (
	"net/http"
	"time"

	"github.com/baby-whales-pod/beeket/internal/version"
)

// StatusResponse is the payload returned by GET /api/status.
type StatusResponse struct {
	Version     string              `json:"version"`
	Commit      string              `json:"commit"`
	Built       string              `json:"built"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	Backend     string              `json:"backend"`
	MaxLoaded   int                 `json:"max_loaded"`
	NumParallel int                 `json:"num_parallel"`
	LoadedModels []StatusModelInfo  `json:"loaded_models"`
}

// StatusModelInfo describes one loaded model in the status response.
type StatusModelInfo struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"last_used"`
}

// Status handles GET /api/status — returns a human-readable JSON status blob.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	loaded := h.sched.LoadedModels()
	models := make([]StatusModelInfo, 0, len(loaded))
	for _, l := range loaded {
		models = append(models, StatusModelInfo{
			Name:     l.Name,
			Size:     l.Size,
			LastUsed: l.LastUsed,
		})
	}

	resp := StatusResponse{
		Version:       version.Version,
		Commit:        version.Commit,
		Built:         version.BuildDate,
		UptimeSeconds: time.Since(h.startTime).Seconds(),
		Backend:       h.backend,
		MaxLoaded:     h.maxLoaded,
		NumParallel:   h.numParallel,
		LoadedModels:  models,
	}
	writeJSON(w, http.StatusOK, resp)
}
