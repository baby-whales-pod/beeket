// Package api — HTTP server wiring.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/baby-whales-pod/beeket/internal/metrics"
)

// Server is the Beeket HTTP server.
type Server struct {
	mux     *http.ServeMux
	handler *Handler
}

// NewServer creates and registers all routes.
func NewServer(h *Handler) *Server {
	s := &Server{
		mux:     http.NewServeMux(),
		handler: h,
	}
	s.routes()
	return s
}

// routes registers all API endpoints.
func (s *Server) routes() {
	h := s.handler

	// Observability — registered before wrapping so /metrics bypasses middleware counting.
	s.mux.Handle("GET /metrics", promhttp.Handler())
	s.mux.HandleFunc("GET /api/status", h.Status)

	// Model management.
	s.mux.HandleFunc("POST /api/pull", h.Pull)
	s.mux.HandleFunc("GET /api/tags", h.Tags)
	s.mux.HandleFunc("POST /api/show", h.Show)
	s.mux.HandleFunc("DELETE /api/delete", h.Delete)
	s.mux.HandleFunc("POST /api/copy", h.Copy)

	// Inference.
	s.mux.HandleFunc("POST /api/generate", h.Generate)
	s.mux.HandleFunc("POST /api/chat", h.Chat)
	s.mux.HandleFunc("POST /api/embeddings", h.Embeddings)

	// Operational.
	s.mux.HandleFunc("GET /api/version", h.Version)
	s.mux.HandleFunc("GET /api/ps", h.PS)
	s.mux.HandleFunc("GET /healthz", h.Healthz)
	s.mux.HandleFunc("GET /readyz", h.Readyz)
}

// ServeHTTP implements http.Handler. The metrics middleware wraps the mux so
// every request (except /metrics itself) is counted and timed.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
	metrics.Middleware(s.mux).ServeHTTP(rw, r)
	slog.Debug("http",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rw.status,
		"duration", time.Since(start).String(),
	)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
