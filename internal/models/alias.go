// Package models manages the Beeket model registry: manifests, aliases,
// metadata, and model-reference normalisation.
package models

// AliasEntry maps a short name to a (name, tag, source) tuple.
// Source is the canonical Hugging Face download URL; MMProjURL is the
// optional vision projector URL for multimodal models.
type AliasEntry struct {
	Name      string
	Tag       string
	Source    string // canonical HF ref
	MMProjURL string // optional vision projector URL
}

// AliasTable holds short-name → AliasEntry mappings.
type AliasTable struct {
	entries map[string]*AliasEntry
}

// DefaultAliases returns the built-in alias table.
func DefaultAliases() *AliasTable {
	t := &AliasTable{entries: make(map[string]*AliasEntry)}
	for _, e := range builtinAliases {
		e := e
		t.entries[e.Name+":"+e.Tag] = &e
		// Also register bare name without tag when tag is "latest".
		if e.Tag == "latest" {
			t.entries[e.Name] = &e
		}
	}
	return t
}

// Lookup returns the AliasEntry for the given key, or nil.
func (t *AliasTable) Lookup(key string) *AliasEntry {
	return t.entries[key]
}

// All returns all built-in alias entries.
func (t *AliasTable) All() []*AliasEntry {
	seen := make(map[*AliasEntry]bool)
	var out []*AliasEntry
	for _, e := range t.entries {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// builtinAliases is the compiled-in alias table.
var builtinAliases = []AliasEntry{
	{
		Name:   "smollm2",
		Tag:    "135m",
		Source: "https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf",
	},
	{
		Name:   "qwen2.5",
		Tag:    "0.5b",
		Source: "https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf",
	},
	{
		Name:   "gemma3",
		Tag:    "1b",
		Source: "https://huggingface.co/google/gemma-3-1b-it-GGUF/resolve/main/gemma-3-1b-it-Q4_K_M.gguf",
	},
	{
		Name:   "nomic-embed-text",
		Tag:    "latest",
		Source: "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf",
	},
}
