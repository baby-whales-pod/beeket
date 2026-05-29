// Package api — HTTP server wiring.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/baby-whales-pod/beeket/internal/metrics"
)

// Server is the Beeket HTTP server. Its ServeHTTP method is the bare mux
// (no metrics middleware here); callers wrap it with metrics.Middleware at
// the http.Server level in main.go.
type Server struct {
	mux     *http.ServeMux
	handler *Handler
}

// NewServer creates and registers all routes.
// metricsEnabled controls whether the GET /metrics route is registered.
func NewServer(h *Handler, metricsEnabled bool) *Server {
	s := &Server{
		mux:     http.NewServeMux(),
		handler: h,
	}
	s.routes(metricsEnabled)
	return s
}

// routes registers all API endpoints.
func (s *Server) routes(metricsEnabled bool) {
	h := s.handler

	// Observability.
	if metricsEnabled {
		s.mux.Handle("GET /metrics", promhttp.Handler())
	}
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
	s.mux.HandleFunc("POST /api/embed", h.Embeddings) // Ollama alias

	// Operational.
	s.mux.HandleFunc("GET /api/version", h.Version)
	s.mux.HandleFunc("GET /api/ps", h.PS)
	s.mux.HandleFunc("GET /healthz", h.Healthz)
	s.mux.HandleFunc("GET /readyz", h.Readyz)
}

// ServeHTTP implements http.Handler. Request logging lives here; metrics
// middleware is applied at the http.Server level in main.go to avoid
// double-wrapping the ResponseWriter.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rw, r)
	slog.Debug("http",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rw.status,
		"duration", time.Since(start).String(),
	)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
// It also implements http.Flusher so streaming SSE / NDJSON responses work.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher, delegating to the underlying writer.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// WrapWithMetrics returns a new http.Handler that wraps srv with the metrics
// middleware when enabled is true. This is the value to assign to
// http.Server.Handler in main.go.
func WrapWithMetrics(srv http.Handler, enabled bool) http.Handler {
	if !enabled {
		return srv
	}
	return metrics.Middleware(srv)
}
