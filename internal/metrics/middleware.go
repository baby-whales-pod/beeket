package metrics

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// requestIDKey is the context key type for request IDs.
type requestIDKey struct{}

// RequestIDFromContext returns the request ID stored in ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// requestIDEncoding is a URL-safe base32 without padding.
var requestIDEncoding = base32.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567").WithPadding(base32.NoPadding)

// newRequestID returns a 16-character cryptographically-random request ID.
func newRequestID() string {
	b := make([]byte, 10) // 10 bytes → 16 base32 chars
	_, _ = rand.Read(b)   //nolint:errcheck // rand.Read never errors on supported platforms
	return requestIDEncoding.EncodeToString(b)
}

// metricsResponseWriter captures the HTTP status code written by a handler
// and implements http.Flusher so streaming responses work correctly.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher, delegating to the wrapped writer if it supports it.
func (rw *metricsResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware wraps an HTTP handler to record:
//   - beeket_http_requests_total (counter)
//   - beeket_http_request_duration_seconds (histogram)
//   - beeket_http_requests_in_flight (gauge)
//
// It also:
//   - Injects a unique X-Request-Id header on every response and stores it in the context.
//   - Excludes the /metrics endpoint itself from request counters to prevent self-amplification.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject / propagate request ID.
		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-Id", reqID)
		// Store in context so downstream handlers can include it in logs.
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, reqID))

		// Skip counting /metrics itself to prevent self-amplification.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Track in-flight requests by method. Deferred Dec ensures correctness
		// even if the handler panics.
		HTTPRequestsInFlight.WithLabelValues(r.Method).Inc()
		defer HTTPRequestsInFlight.WithLabelValues(r.Method).Dec()

		rw := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		// r.Pattern is populated by ServeMux *during* routing (inside next.ServeHTTP).
		next.ServeHTTP(rw, r)

		dur := time.Since(start).Seconds()

		// Read r.Pattern now — it's set after ServeMux routing completes.
		pattern := r.Pattern
		if pattern == "" {
			pattern = sanitisePath(r.URL.Path)
		}

		// Skip recording the /metrics route if somehow it reaches this point.
		if pattern == "GET /metrics" {
			return
		}

		code := strconv.Itoa(rw.status)
		HTTPRequestsTotal.WithLabelValues(r.Method, pattern, code).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(dur)
	})
}

// sanitisePath returns a safe fallback pattern label from a raw URL path.
func sanitisePath(path string) string {
	if path == "" {
		return "/"
	}
	return fmt.Sprintf("UNKNOWN %s", path)
}
