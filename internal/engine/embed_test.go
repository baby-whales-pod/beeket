package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// l2Normalize
// ---------------------------------------------------------------------------

func TestL2Normalize_UnitVector(t *testing.T) {
	v := []float32{1, 0, 0}
	l2Normalize(v)
	assert.InDelta(t, 1.0, v[0], 1e-6)
	assert.InDelta(t, 0.0, v[1], 1e-6)
	assert.InDelta(t, 0.0, v[2], 1e-6)
}

func TestL2Normalize_ArbitraryVector(t *testing.T) {
	v := []float32{3, 4}
	l2Normalize(v)
	// Expected: [3/5, 4/5]
	assert.InDelta(t, 0.6, float64(v[0]), 1e-6)
	assert.InDelta(t, 0.8, float64(v[1]), 1e-6)
	// L2 norm of result must be 1.
	norm := math.Sqrt(float64(v[0])*float64(v[0]) + float64(v[1])*float64(v[1]))
	assert.InDelta(t, 1.0, norm, 1e-6)
}

func TestL2Normalize_ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	l2Normalize(v) // must not panic; vector unchanged
	assert.Equal(t, []float32{0, 0, 0}, v)
}

func TestL2Normalize_NormIsOne(t *testing.T) {
	v := []float32{1, 2, 3, 4, 5}
	l2Normalize(v)
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	assert.InDelta(t, 1.0, math.Sqrt(sumSq), 1e-5)
}

// ---------------------------------------------------------------------------
// EmbedSession constructor (unit test without FFI)
// The real FFI path is tested in integration tests gated on BEEKET_E2E_MODEL.
// Here we only test the l2Normalize helper exhaustively.
// ---------------------------------------------------------------------------

func TestEmbedSession_NilModelReturnsError(t *testing.T) {
	t.Skip("FFI integration path requires BEEKET_E2E_MODEL and a real llama.cpp library")
}

func TestL2Normalize_SingleElement(t *testing.T) {
	v := []float32{5}
	l2Normalize(v)
	assert.InDelta(t, 1.0, float64(v[0]), 1e-6)
}

func TestL2Normalize_NegativeValues(t *testing.T) {
	v := []float32{-3, -4}
	l2Normalize(v)
	norm := math.Sqrt(float64(v[0])*float64(v[0]) + float64(v[1])*float64(v[1]))
	assert.InDelta(t, 1.0, norm, 1e-6)
	assert.Less(t, float64(v[0]), 0.0, "sign should be preserved")
}
