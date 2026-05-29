package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanModelRef_HFQuantShorthand(t *testing.T) {
	tests := []struct {
		ref      string
		wantName string
		wantTag  string
	}{
		{
			// Standard compound-GGUF suffix + quant
			ref:      "hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M",
			wantName: "qwen3.5-0.8b",
			wantTag:  "q4_k_m",
		},
		{
			// Instruct descriptor stripped
			ref:      "hf.co/bartowski/Llama-3-8B-Instruct-GGUF:Q5_K_M",
			wantName: "llama-3-8b",
			wantTag:  "q5_k_m",
		},
		{
			// Plain -GGUF suffix (no descriptor)
			ref:      "hf.co/QuantFactory/SmolLM2-135M-GGUF:Q4_K_M",
			wantName: "smollm2-135m",
			wantTag:  "q4_k_m",
		},
		{
			// Chat descriptor stripped; version segment with digit preserved
			ref:      "hf.co/org/Mistral-7B-Chat-GGUF:Q8_0",
			wantName: "mistral-7b",
			wantTag:  "q8_0",
		},
		{
			// Version suffix -v2 contains digit → preserved
			ref:      "hf.co/org/Mistral-7B-v2-GGUF:Q4_K_M",
			wantName: "mistral-7b-v2",
			wantTag:  "q4_k_m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, tag := CleanModelRef(tt.ref)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestCleanModelRef_HFExplicitFile(t *testing.T) {
	name, tag := CleanModelRef("hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/Qwen3.5-0.8B-Q4_K_M.gguf")
	// For explicit file form, name comes from the repo (same stripping), tag from filename stem.
	assert.Equal(t, "qwen3.5-0.8b", name)
	assert.Equal(t, "qwen3.5-0.8b-q4_k_m", tag)
}

func TestCleanModelRef_HFBareRepo(t *testing.T) {
	name, tag := CleanModelRef("hf.co/QuantFactory/SmolLM2-135M-GGUF")
	assert.Equal(t, "smollm2-135m", name)
	assert.Equal(t, "latest", tag)
}

func TestCleanModelRef_DirectHTTPS(t *testing.T) {
	name, tag := CleanModelRef("https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf")
	assert.Equal(t, "smollm2-135m.q4_k_m", name) // stem of the filename, lowercased
	assert.Equal(t, "latest", tag)
}

func TestCleanModelRef_ShortNameTag(t *testing.T) {
	name, tag := CleanModelRef("smollm2:135m")
	assert.Equal(t, "smollm2", name)
	assert.Equal(t, "135m", tag)
}

func TestCleanModelRef_BareName(t *testing.T) {
	name, tag := CleanModelRef("smollm2")
	assert.Equal(t, "smollm2", name)
	assert.Equal(t, "latest", tag)
}

// TestCleanModelRef_NoSlashes verifies the name is always slash-free,
// which is the core invariant needed by store.WriteManifest.
func TestCleanModelRef_NoSlashes(t *testing.T) {
	inputs := []string{
		"hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M",
		"hf.co/bartowski/Llama-3-8B-Instruct-GGUF:Q5_K_M",
		"https://huggingface.co/org/repo/resolve/main/model.gguf",
		"hf.co/org/repo/model.gguf",
	}
	for _, ref := range inputs {
		name, _ := CleanModelRef(ref)
		assert.NotContains(t, name, "/", "name must be slash-free for ref=%q", ref)
	}
}
