package download

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// guessFilename
// ---------------------------------------------------------------------------

func TestGuessFilename_PlainGGUFSuffix(t *testing.T) {
	// SmolLM2-135M-GGUF: "-135M" contains digits so is NOT stripped as a descriptor.
	// Only "-GGUF" is removed → SmolLM2-135M-Q4_K_M.gguf
	got := guessFilename("SmolLM2-135M-GGUF", "Q4_K_M")
	assert.Equal(t, "SmolLM2-135M-Q4_K_M.gguf", got)
}

func TestGuessFilename_CompoundMTPGGUF(t *testing.T) {
	// Qwen3.5-0.8B-MTP-GGUF → Qwen3.5-0.8B-Q4_K_M.gguf
	got := guessFilename("Qwen3.5-0.8B-MTP-GGUF", "Q4_K_M")
	assert.Equal(t, "Qwen3.5-0.8B-Q4_K_M.gguf", got)
}

func TestGuessFilename_CompoundInstructGGUF(t *testing.T) {
	// Llama-3.2-1B-Instruct-GGUF → Llama-3.2-1B-Q4_K_M.gguf
	got := guessFilename("Llama-3.2-1B-Instruct-GGUF", "Q4_K_M")
	assert.Equal(t, "Llama-3.2-1B-Q4_K_M.gguf", got)
}

func TestGuessFilename_CompoundChatGGUF(t *testing.T) {
	// Mistral-7B-Chat-GGUF → Mistral-7B-Q4_K_M.gguf
	got := guessFilename("Mistral-7B-Chat-GGUF", "Q4_K_M")
	assert.Equal(t, "Mistral-7B-Q4_K_M.gguf", got)
}

func TestGuessFilename_DifferentQuant(t *testing.T) {
	// "-2B" contains a digit so is NOT stripped; only "-GGUF" removed.
	got := guessFilename("Gemma-2B-GGUF", "Q8_0")
	assert.Equal(t, "Gemma-2B-Q8_0.gguf", got)
}

func TestGuessFilename_CaseInsensitiveGGUF(t *testing.T) {
	// Regex is case-insensitive
	got := guessFilename("Model-gguf", "Q4_K_M")
	assert.Equal(t, "Model-Q4_K_M.gguf", got)
}

func TestGuessFilename_NoGGUFSuffix(t *testing.T) {
	// Repo name with no GGUF suffix — treated as-is
	got := guessFilename("my-model", "Q4_K_M")
	assert.Equal(t, "my-model-Q4_K_M.gguf", got)
}

// ---------------------------------------------------------------------------
// TmpFilename
// ---------------------------------------------------------------------------

func TestTmpFilename_BasicGGUFURL(t *testing.T) {
	url := "https://huggingface.co/org/repo/resolve/main/model.gguf"
	got := TmpFilename(url)
	assert.Equal(t, "model.gguf.tmp", got)
	// Must not produce double .gguf
	assert.NotContains(t, got, ".gguf.gguf")
}

func TestTmpFilename_URLWithoutExtension(t *testing.T) {
	url := "https://huggingface.co/org/repo/resolve/main/model"
	got := TmpFilename(url)
	assert.Equal(t, "model.gguf.tmp", got)
}

func TestTmpFilename_ComplexPath(t *testing.T) {
	url := "https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf"
	got := TmpFilename(url)
	assert.Equal(t, "Qwen3.5-0.8B-Q4_K_M.gguf.tmp", got)
	assert.NotContains(t, got, ".gguf.gguf")
}

func TestTmpFilename_NoURLPathSlashes(t *testing.T) {
	// Result must be a flat filename with no slashes
	url := "https://huggingface.co/org/repo/resolve/main/model.gguf"
	got := TmpFilename(url)
	assert.NotContains(t, got, "/")
}

// ---------------------------------------------------------------------------
// resolveHF
// ---------------------------------------------------------------------------

func TestResolveHF_ExplicitFile(t *testing.T) {
	got := resolveHF("unsloth/Qwen3.5-0.8B-MTP-GGUF/Qwen3.5-0.8B-Q4_K_M.gguf")
	assert.Equal(t, "https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf", got)
}

func TestResolveHF_QuantShorthand(t *testing.T) {
	got := resolveHF("unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M")
	// Should produce a URL ending with the guessed filename
	assert.Contains(t, got, "https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/")
	assert.Contains(t, got, "Q4_K_M.gguf")
	// Crucially, the base should strip -MTP-GGUF not just -GGUF
	assert.Contains(t, got, "Qwen3.5-0.8B-Q4_K_M.gguf")
}

func TestResolveHF_DefaultQuant(t *testing.T) {
	got := resolveHF("QuantFactory/SmolLM2-135M-GGUF")
	assert.Contains(t, got, "Q4_K_M.gguf")
	assert.Contains(t, got, "https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/")
}

// ---------------------------------------------------------------------------
// Resolve (public entry point)
// ---------------------------------------------------------------------------

func TestResolve_DirectHTTPS(t *testing.T) {
	u := "https://example.com/model.gguf"
	got, err := Resolve(u)
	assert.NoError(t, err)
	assert.Equal(t, u, got)
}

func TestResolve_HFShorthand(t *testing.T) {
	got, err := Resolve("hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M")
	assert.NoError(t, err)
	assert.Contains(t, got, "huggingface.co")
	assert.Contains(t, got, "Qwen3.5-0.8B-Q4_K_M.gguf")
}

func TestResolve_UnknownScheme(t *testing.T) {
	_, err := Resolve("ollama://model:tag")
	assert.Error(t, err)
}
