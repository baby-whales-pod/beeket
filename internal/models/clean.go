package models

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// ggufSuffixRe strips trailing GGUF-variant suffixes from HF repo names.
// It removes an optional purely-alphabetic descriptor segment before -GGUF,
// so compound forms like -MTP-GGUF, -Instruct-GGUF, -Chat-GGUF are handled.
// Segments containing digits (e.g. -135M, -0.8B) are preserved.
var ggufSuffixRe = regexp.MustCompile(`(?i)(?:-[A-Za-z]+)?-GGUF$`)

// CleanModelRef derives a clean (name, tag) registry key from any model
// reference accepted by beeket pull. The returned name is slash-free and
// lowercase, safe to use as a single-level directory name in the manifest
// store.
//
// Supported input forms:
//
//	hf.co/<org>/<repo>:<quant>       → name=stripped-lower(repo), tag=lower(quant)
//	hf.co/<org>/<repo>/<file>.gguf   → name=stripped-lower(repo), tag=stem(file)
//	hf.co/<org>/<repo>               → name=stripped-lower(repo), tag="latest"
//	https://…/<file>.gguf            → name=stem(file),           tag="latest"
//	<name>:<tag>  (short form)        → name=name, tag=tag   (passthrough)
//	<bare-name>                       → name=bare, tag="latest"
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

// cleanHFRef handles everything after the "hf.co/" prefix.
func cleanHFRef(path string) (name, tag string) {
	parts := strings.SplitN(path, "/", 3) // [org, repo] or [org, repo, rest]

	var repoName string
	if len(parts) >= 2 {
		repoName = parts[1]
	} else {
		// Bare org — unlikely but handle gracefully.
		return strings.ToLower(path), "latest"
	}

	base := strings.ToLower(ggufSuffixRe.ReplaceAllString(repoName, ""))

	// hf.co/<org>/<repo>/<file>.gguf
	if len(parts) == 3 && strings.HasSuffix(parts[2], ".gguf") {
		tag := strings.ToLower(strings.TrimSuffix(parts[2], ".gguf"))
		return base, tag
	}

	// hf.co/<org>/<repo>:<quant>  — the colon is still in path at this point
	// because SplitN("/", 3) doesn't split on colon.
	// We need to check if repoName itself ends with :<quant>.
	// Actually Resolve splits on the LAST colon in the original ref, but here
	// we already stripped "hf.co/" so path looks like "org/repo:quant".
	// SplitN(..., 3) on "/" gives ["org", "repo:quant"] when there are only 2
	// slash-separated parts.
	if len(parts) == 2 {
		repoAndQuant := parts[1]
		if idx := strings.LastIndex(repoAndQuant, ":"); idx > 0 {
			repoName = repoAndQuant[:idx]
			quant := repoAndQuant[idx+1:]
			base = strings.ToLower(ggufSuffixRe.ReplaceAllString(repoName, ""))
			return base, strings.ToLower(quant)
		}
	}

	// hf.co/<org>/<repo> — no quant, no file
	return base, "latest"
}

// cleanHTTPRef handles direct https:// / http:// download URLs.
func cleanHTTPRef(rawURL string) (name, tag string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback: use whole URL sanitized.
		safe := strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(rawURL)
		return strings.ToLower(safe), "latest"
	}
	// Use last path segment stem as name.
	base := filepath.Base(u.Path)
	base = strings.TrimSuffix(base, ".gguf")
	return strings.ToLower(base), "latest"
}
