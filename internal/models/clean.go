package models

import (
	"net/url"
	"path/filepath"
	"strings"
)

// CleanModelRef derives a clean (name, tag) registry key from any model
// reference accepted by beeket pull. The returned name and tag are
// slash-free and lowercase, safe to use as directory names in the manifest
// store (which expects exactly two levels: manifests/<name>/<tag>.json).
//
// Supported input forms:
//
//	hf.co/<org>/<repo>:<quant>       → name=stripped-lower(repo), tag=lower(quant)
//	hf.co/<org>/<repo>/<file>.gguf   → name=stripped-lower(repo), tag=lower(stem(file))
//	hf.co/<org>/<repo>               → name=stripped-lower(repo), tag="latest"
//	https://…/<file>.gguf            → name=lower(stem),           tag="latest"
//	<name>:<tag>  (short form)        → name=lower(name), tag=lower(tag)
//	<bare-name>                       → name=lower(bare), tag="latest"
func CleanModelRef(ref string) (name, tag string) {
	// --- hf.co/ shorthand ---
	if hf, ok := strings.CutPrefix(ref, "hf.co/"); ok {
		return cleanHFRef(hf)
	}

	// --- Direct HTTPS/HTTP URL ---
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return cleanHTTPRef(ref)
	}

	// --- Short name:tag or bare name (aliases, already-clean names) ---
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		return strings.ToLower(ref[:idx]), strings.ToLower(ref[idx+1:])
	}
	return strings.ToLower(ref), "latest"
}

// cleanHFRef handles the path portion after the "hf.co/" prefix.
func cleanHFRef(path string) (name, tag string) {
	parts := strings.SplitN(path, "/", 3) // [org, repo] or [org, repo, rest]

	if len(parts) < 2 {
		// Bare org — unlikely but handle gracefully.
		return strings.ToLower(path), "latest"
	}

	// hf.co/<org>/<repo>:<quant> — colon appears inside parts[1] because
	// SplitN on "/" doesn't split on ":". Detect this first.
	if len(parts) == 2 {
		repoAndQuant := parts[1]
		if idx := strings.LastIndex(repoAndQuant, ":"); idx > 0 {
			repoName := repoAndQuant[:idx]
			quant := repoAndQuant[idx+1:]
			base := strings.ToLower(StripGGUFSuffix(repoName))
			return base, strings.ToLower(quant)
		}
	}

	repoName := parts[1]
	base := strings.ToLower(StripGGUFSuffix(repoName))

	// hf.co/<org>/<repo>/<file>.gguf (possibly with subdirs: .../subdir/file.gguf)
	if len(parts) == 3 && strings.HasSuffix(parts[2], ".gguf") {
		// Use filepath.Base to strip any subdirectory path — ensures the tag
		// is slash-free even for paths like "refs/pr/1/model.gguf".
		stem := filepath.Base(parts[2])
		stem = strings.TrimSuffix(stem, ".gguf")
		return base, strings.ToLower(stem)
	}

	// hf.co/<org>/<repo> — no quant, no file
	return base, "latest"
}

// cleanHTTPRef handles direct https:// / http:// download URLs.
func cleanHTTPRef(rawURL string) (name, tag string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback: sanitize the whole URL into a flat name.
		safe := strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(rawURL)
		return strings.ToLower(safe), "latest"
	}
	// Use last path segment stem as name — filepath.Base handles query/fragment
	// stripping because url.Parse already separates them from u.Path.
	base := filepath.Base(u.Path)
	base = strings.TrimSuffix(base, ".gguf")
	return strings.ToLower(base), "latest"
}
