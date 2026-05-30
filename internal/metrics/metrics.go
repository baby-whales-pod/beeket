// Package metrics declares and registers all Prometheus collectors for beeket.
// All metrics are registered on the default prometheus.Registry so that
// promhttp.Handler() picks them up without extra wiring.
package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTP metrics.
var (
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "beeket_http_requests_total",
		Help: "Total number of HTTP requests handled.",
	}, []string{"method", "pattern", "status_code"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "beeket_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "pattern"})

	HTTPRequestsInFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "beeket_http_requests_in_flight",
		Help: "Number of HTTP requests currently being processed, by HTTP method.",
	}, []string{"method"})
)

// Inference metrics.
var (
	InferenceRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "beeket_inference_requests_total",
		Help: "Total number of inference requests by outcome.",
	}, []string{"model", "endpoint", "outcome"})

	InferenceTTFT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "beeket_inference_time_to_first_token_seconds",
		Help:    "Time from request receipt to first generated token.",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"model"})

	// InferenceEvalTokensTotal counts generated (eval) tokens.
	// Derive throughput with rate(beeket_inference_eval_tokens_total[1m]).
	InferenceEvalTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "beeket_inference_eval_tokens_total",
		Help: "Total number of tokens generated (eval) by the inference engine.",
	}, []string{"model"})

	InferenceDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "beeket_inference_duration_seconds",
		Help:    "End-to-end inference request duration.",
		Buckets: prometheus.DefBuckets,
	}, []string{"model"})
)

// Model lifecycle metrics.
var (
	ModelsLoaded = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "beeket_models_loaded",
		Help: "Number of models currently loaded in memory.",
	})

	ModelLoadDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "beeket_model_load_duration_seconds",
		Help:    "Time to load a model into the inference engine.",
		Buckets: []float64{.1, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"model"})

	ModelEvictionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "beeket_model_evictions_total",
		Help: "Total number of model evictions by reason (lru, idle).",
	}, []string{"reason"})
)

// Build / uptime info metrics.
var (
	BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "beeket_build_info",
		Help: "Build information (always 1). Labels carry version metadata.",
	}, []string{"version", "commit", "built"})

	UptimeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "beeket_uptime_seconds",
		Help: "Seconds since beeket serve started.",
	})
)

// registerOnce ensures Register() is idempotent — safe to call multiple times
// (e.g. from tests, or if the server is restarted in-process).
var registerOnce sync.Once

// Register registers all custom beeket collectors on the default Prometheus
// registry. It is idempotent: subsequent calls are no-ops.
//
// Note: prometheus/client_golang's own init() already registers the Go runtime
// and process collectors (NewGoCollector, NewProcessCollector) on
// DefaultRegisterer — we must NOT register them again or we get:
//
//	panic: duplicate metrics collector registration attempted
func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(
			// HTTP
			HTTPRequestsTotal,
			HTTPRequestDuration,
			HTTPRequestsInFlight,
			// Inference
			InferenceRequestsTotal,
			InferenceTTFT,
			InferenceEvalTokensTotal,
			InferenceDuration,
			// Model lifecycle
			ModelsLoaded,
			ModelLoadDuration,
			ModelEvictionsTotal,
			// Build info / uptime
			BuildInfo,
			UptimeSeconds,
			// Go runtime and process collectors are already registered by
			// prometheus/client_golang's init() — do not register them here.
		)
	})
}

// SetBuildInfo sets the beeket_build_info gauge (call once at startup).
func SetBuildInfo(ver, commit, built string) {
	BuildInfo.WithLabelValues(ver, commit, built).Set(1)
}

// StartUptimeTicker starts a background goroutine that updates the
// beeket_uptime_seconds gauge every second. The goroutine exits when ctx
// is cancelled, preventing goroutine leaks on server shutdown.
func StartUptimeTicker(ctx context.Context, startTime time.Time) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				UptimeSeconds.Set(time.Since(startTime).Seconds())
			}
		}
	}()
}

// InferenceOutcome constants match the `outcome` label values used in
// InferenceRequestsTotal. Use these instead of raw strings to avoid typos.
const (
	// OutcomeSuccess labels requests that completed normally.
	OutcomeSuccess = "success"
	// OutcomeError labels requests that failed with an error.
	OutcomeError = "error"
	// OutcomeCancelled labels requests cancelled by the client.
	OutcomeCancelled = "cancelled"
)
