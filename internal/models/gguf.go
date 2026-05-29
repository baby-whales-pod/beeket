package models

import "regexp"

// GGUFSuffixRe matches trailing GGUF-variant suffixes in HuggingFace repo names.
// It strips an optional purely-alphabetic descriptor segment before "-GGUF",
// handling compound suffixes like:
//
//	-GGUF          → strip only "-GGUF"
//	-MTP-GGUF      → strip "-MTP-GGUF"     (MTP is alpha-only)
//	-Instruct-GGUF → strip "-Instruct-GGUF"
//	-Chat-GGUF     → strip "-Chat-GGUF"
//
// Segments containing digits (e.g. "-135M", "-0.8B", "-v0.1") are NOT stripped
// because they are part of the model's base name (size/version identifiers).
// Example: "Mixtral-8x7B-Instruct-v0.1-GGUF" → "Mixtral-8x7B-Instruct-v0.1"
// (Instruct is preserved because -v0.1 sits between it and -GGUF).
var GGUFSuffixRe = regexp.MustCompile(`(?i)(?:-[A-Za-z]+)?-GGUF$`)

// StripGGUFSuffix removes the trailing GGUF variant suffix from a HF repo name.
func StripGGUFSuffix(repoName string) string {
	return GGUFSuffixRe.ReplaceAllString(repoName, "")
}
