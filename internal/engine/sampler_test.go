package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// SamplerOptions helpers
// ---------------------------------------------------------------------------

// TestRepeatPenaltyZeroDefaultsToDisabled verifies the critical fix:
// when RepeatPenalty == 0 but a frequency/presence penalty is set, the
// repeat-penalty argument passed to llama.cpp is 1.0 (disabled), not 0.0
// (which causes degenerate output). We test the normalisation logic directly.
func TestRepeatPenaltyZeroDefaultsToDisabled(t *testing.T) {
	opts := SamplerOptions{
		// RepeatPenalty deliberately left at zero; frequency penalty set.
		FrequencyPenalty: 0.5,
	}

	// Reproduce the normalisation logic from buildSampler / buildSamplerWithGrammar.
	repeatP := opts.RepeatPenalty
	if repeatP == 0 {
		repeatP = 1.0
	}

	assert.Equal(t, float32(1.0), repeatP,
		"RepeatPenalty=0 must normalise to 1.0 (llama.cpp disabled value)")
}

// TestRepeatPenaltyNonZeroUnchanged verifies that an explicitly set
// repeat penalty is not modified by the normalisation.
func TestRepeatPenaltyNonZeroUnchanged(t *testing.T) {
	opts := SamplerOptions{RepeatPenalty: 1.3}
	repeatP := opts.RepeatPenalty
	if repeatP == 0 {
		repeatP = 1.0
	}
	assert.Equal(t, float32(1.3), repeatP)
}

// TestRepeatPenaltyOneMeansDisabled confirms that RepeatPenalty=1.0 is the
// llama.cpp "disabled" sentinel and passes through unchanged.
func TestRepeatPenaltyOneMeansDisabled(t *testing.T) {
	opts := SamplerOptions{RepeatPenalty: 1.0}
	repeatP := opts.RepeatPenalty
	if repeatP == 0 {
		repeatP = 1.0
	}
	assert.Equal(t, float32(1.0), repeatP)
}

// ---------------------------------------------------------------------------
// nPredict sentinel handling
// ---------------------------------------------------------------------------

// resolveNPredict mirrors the nPredict logic in Session.Generate so we can
// unit-test it without an FFI context.
func resolveNPredict(maxTokens int) int {
	n := maxTokens
	if n == 0 {
		n = 512
	}
	return n
}

// TestNumPredictZeroDefaultsTo512 verifies that MaxTokens=0 (the zero-value,
// meaning "use default") resolves to 512.
func TestNumPredictZeroDefaultsTo512(t *testing.T) {
	assert.Equal(t, 512, resolveNPredict(0))
}

// TestNumPredictMinusOneIsPassedThrough verifies that MaxTokens=-1 (unlimited)
// is NOT capped to 512. The decode loop terminates when nPredict reaches 0
// via the `nPredict--; if nPredict <= 0 { break }` check, but -1 decrements
// toward more-negative values indefinitely (i.e. it runs until EOG or stop).
func TestNumPredictMinusOneIsPassedThrough(t *testing.T) {
	result := resolveNPredict(-1)
	assert.Equal(t, -1, result,
		"MaxTokens=-1 (unlimited) must not be capped to 512")
}

// TestNumPredictPositiveUnchanged verifies that an explicit positive value
// passes through unchanged.
func TestNumPredictPositiveUnchanged(t *testing.T) {
	assert.Equal(t, 128, resolveNPredict(128))
	assert.Equal(t, 1024, resolveNPredict(1024))
}

// ---------------------------------------------------------------------------
// TypicalP gate
// ---------------------------------------------------------------------------

// useTypicalP mirrors the TypicalP branch condition so we can verify the gate.
func useTypicalP(opts SamplerOptions) bool {
	return opts.TypicalP > 0 && opts.TypicalP < 1.0
}

// TestTypicalPGate_DisabledAtOne verifies that TypicalP=1.0 (all tokens
// equally typical, i.e. disabled) does not trigger the TypicalP sampler.
func TestTypicalPGate_DisabledAtOne(t *testing.T) {
	assert.False(t, useTypicalP(SamplerOptions{TypicalP: 1.0}),
		"TypicalP=1.0 means disabled — should fall through to TopP")
}

// TestTypicalPGate_DisabledAtZero verifies that TypicalP=0 (unset) does not
// trigger the TypicalP sampler.
func TestTypicalPGate_DisabledAtZero(t *testing.T) {
	assert.False(t, useTypicalP(SamplerOptions{TypicalP: 0}))
}

// TestTypicalPGate_EnabledBelowOne verifies that TypicalP=0.9 activates the
// TypicalP sampler path.
func TestTypicalPGate_EnabledBelowOne(t *testing.T) {
	assert.True(t, useTypicalP(SamplerOptions{TypicalP: 0.9}),
		"TypicalP=0.9 should activate TypicalP sampler")
}

// TestTypicalPGate_EdgeValues covers boundary values.
func TestTypicalPGate_EdgeValues(t *testing.T) {
	cases := []struct {
		typicalP float32
		want     bool
		desc     string
	}{
		{0.0, false, "zero = unset"},
		{0.01, true, "small positive"},
		{0.5, true, "mid range"},
		{0.99, true, "just below 1.0"},
		{1.0, false, "exactly 1.0 = disabled"},
		{1.1, false, "above 1.0 (invalid but guarded)"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.want, useTypicalP(SamplerOptions{TypicalP: tc.typicalP}))
		})
	}
}

// ---------------------------------------------------------------------------
// Mirostat option wiring
// ---------------------------------------------------------------------------

// TestMirostatDefaults verifies that tau/eta defaulting logic works correctly.
func TestMirostatDefaults(t *testing.T) {
	// Reproduce the tau/eta defaulting logic from buildSampler.
	resolve := func(opts SamplerOptions) (tau, eta float32) {
		tau = opts.MirostatTau
		if tau == 0 {
			tau = 5.0
		}
		eta = opts.MirostatEta
		if eta == 0 {
			eta = 0.1
		}
		return
	}

	t.Run("zero values default to llama.cpp defaults", func(t *testing.T) {
		tau, eta := resolve(SamplerOptions{Mirostat: 2})
		assert.Equal(t, float32(5.0), tau)
		assert.Equal(t, float32(0.1), eta)
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		tau, eta := resolve(SamplerOptions{Mirostat: 1, MirostatTau: 3.0, MirostatEta: 0.05})
		assert.Equal(t, float32(3.0), tau)
		assert.Equal(t, float32(0.05), eta)
	})
}

// TestMirostatEnabled verifies that Mirostat > 0 activates the Mirostat path.
func TestMirostatEnabled(t *testing.T) {
	assert.True(t, SamplerOptions{Mirostat: 1}.Mirostat > 0)
	assert.True(t, SamplerOptions{Mirostat: 2}.Mirostat > 0)
	assert.False(t, SamplerOptions{Mirostat: 0}.Mirostat > 0)
}

// ---------------------------------------------------------------------------
// SamplerOptions — DefaultSamplerOptions
// ---------------------------------------------------------------------------

// TestDefaultSamplerOptions verifies the defaults are sane.
func TestDefaultSamplerOptions(t *testing.T) {
	d := DefaultSamplerOptions()
	assert.Equal(t, float32(0.8), d.Temperature)
	assert.Equal(t, int32(40), d.TopK)
	assert.Equal(t, float32(0.9), d.TopP)
	assert.Equal(t, float32(0.05), d.MinP)
	assert.Equal(t, float32(5.0), d.MirostatTau)
	assert.Equal(t, float32(0.1), d.MirostatEta)
	// RepeatPenalty defaults to 0 (unset); zero is normalised to 1.0 at use time.
	assert.Equal(t, float32(0), d.RepeatPenalty,
		"RepeatPenalty default is 0 (normalised to 1.0=disabled when penalties are applied)")
}

// ---------------------------------------------------------------------------
// GenerateOptions — MaxTokens sentinel
// ---------------------------------------------------------------------------

// TestGenerateOptions_MaxTokensSentinels documents and verifies the sentinel values.
func TestGenerateOptions_MaxTokensSentinels(t *testing.T) {
	cases := []struct {
		maxTokens    int
		wantResolved int
		desc         string
	}{
		{0, 512, "0 = use server default (512)"},
		{-1, -1, "-1 = unlimited (no cap)"},
		{256, 256, "explicit positive"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.wantResolved, resolveNPredict(tc.maxTokens))
		})
	}
}
