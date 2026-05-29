package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/baby-whales-pod/beeket/internal/metrics"
)

// newTestRegistry creates isolated metric instances for testing,
// independent of the default global registry.
func newTestCounterVec(name string, labels []string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name}, labels)
}

// TestMiddleware_CountsRequests verifies that the middleware increments
// HTTPRequestsTotal for each handled request.
func TestMiddleware_CountsRequests(t *testing.T) {
	// Use the package-level metrics (already registered in init via Register).
	// We reset them by calling a fresh test-scoped registry via direct counter ops.
	// Instead, we exercise the middleware with a real handler and check the
	// counter delta via testutil.ToFloat64.

	// Build a minimal mux with a known route.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := metrics.Middleware(mux)

	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "GET /api/test", "200"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "GET /api/test", "200"),
	)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.InDelta(t, 1.0, after-before, 0.001, "counter should increment by 1")
}

// TestMiddleware_SetsRequestID verifies that a request-ID header is injected.
func TestMiddleware_SetsRequestID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := metrics.Middleware(mux)
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	rid := rr.Header().Get("X-Request-Id")
	require.NotEmpty(t, rid, "X-Request-Id header must be set")
	assert.Len(t, rid, 16, "request ID should be 16 chars (base32 of 10 random bytes)")
}

// TestMiddleware_PropagatesExistingRequestID verifies that a pre-set
// X-Request-Id is echoed back unchanged.
func TestMiddleware_PropagatesExistingRequestID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := metrics.Middleware(mux)
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-Id", "MY-CUSTOM-ID")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "MY-CUSTOM-ID", rr.Header().Get("X-Request-Id"))
}

// TestMiddleware_SkipsMetricsEndpoint verifies that requests to /metrics
// do not increment the request counter (no self-amplification).
func TestMiddleware_SkipsMetricsEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := metrics.Middleware(mux)

	// Capture all current label combinations for the metrics path.
	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/metrics", "200"),
	)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/metrics", "200"),
	)

	assert.Equal(t, before, after, "/metrics requests must not be counted")
}

// TestMiddleware_InFlightGaugeDecrements verifies that the in-flight gauge
// returns to its prior value after a request completes.
func TestMiddleware_InFlightGaugeDecrements(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/slow", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := metrics.Middleware(mux)

	before := testutil.ToFloat64(metrics.HTTPRequestsInFlight.WithLabelValues("GET"))

	req := httptest.NewRequest(http.MethodGet, "/api/slow", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	after := testutil.ToFloat64(metrics.HTTPRequestsInFlight.WithLabelValues("GET"))
	assert.Equal(t, before, after, "in-flight gauge should return to prior value after request")
}

// TestMiddleware_StatusCode verifies non-200 status codes are labelled correctly.
func TestMiddleware_StatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/notfound", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := metrics.Middleware(mux)

	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "GET /api/notfound", "404"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/notfound", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "GET /api/notfound", "404"),
	)
	assert.InDelta(t, 1.0, after-before, 0.001, "404 counter should increment")
}
