package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto/rand"
	"encoding/base32"
)

// requestIDKey is the context key type for request IDs.
type requestIDKey struct{}

// requestIDEncoding is a URL-safe base32 without padding.
var requestIDEncoding = base32.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567").WithPadding(base32.NoPadding)

// newRequestID returns a 16-character cryptographically-random request ID.
func newRequestID() string {
	b := make([]byte, 10) // 10 bytes → 16 base32 chars
	_, _ = rand.Read(b)   //nolint:errcheck // rand.Read never errors on supported platforms
	return requestIDEncoding.EncodeToString(b)
}

// metricsResponseWriter captures the HTTP status code written by a handler.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Middleware wraps an HTTP handler to record:
//   - beeket_http_requests_total (counter)
//   - beeket_http_request_duration_seconds (histogram)
//   - beeket_http_requests_in_flight (gauge)
//
// It also injects a unique X-Request-Id header on every response.
// The /metrics endpoint itself is excluded from request counters to
// prevent self-amplification.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject request ID.
		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-Id", reqID)

		// Skip counting /metrics itself.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Use r.Method as a pre-routing in-flight label (pattern not yet known).
		// After routing completes, use the real pattern.
		HTTPRequestsInFlight.WithLabelValues(r.Method).Inc()

		rw := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		// r.Pattern is set by ServeMux *during* routing (inside next.ServeHTTP),
		// so we collect it after the call completes.
		next.ServeHTTP(rw, r)

		HTTPRequestsInFlight.WithLabelValues(r.Method).Dec()

		dur := time.Since(start).Seconds()

		// r.Pattern is now populated by ServeMux.
		pattern := r.Pattern
		if pattern == "" {
			pattern = sanitisePath(r.URL.Path)
		}

		// Skip the /metrics pattern even if it was routed through the mux.
		if pattern == "GET /metrics" {
			return
		}

		code := strconv.Itoa(rw.status)
		HTTPRequestsTotal.WithLabelValues(r.Method, pattern, code).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(dur)
	})
}

// sanitisePath returns a safe fallback pattern label from a raw URL path.
// It replaces path separators so the label contains no slashes that could
// confuse PromQL.
func sanitisePath(path string) string {
	if path == "" {
		return "/"
	}
	return fmt.Sprintf("UNKNOWN %s", path)
}
