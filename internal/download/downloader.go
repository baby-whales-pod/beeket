// Package download handles model downloads from Hugging Face and direct URLs.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Progress is called periodically during a download.
type Progress func(downloaded, total int64)

// Resolver translates a model reference string into a direct download URL.
func Resolve(ref string) (string, error) {
	// Direct HTTPS URL.
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return ref, nil
	}
	// Local file.
	if strings.HasPrefix(ref, "file://") {
		return ref, nil
	}
	// Hugging Face shorthand: hf.co/<org>/<repo>[:<quant>] or hf.co/<org>/<repo>/<file>.gguf
	if strings.HasPrefix(ref, "hf.co/") {
		return resolveHF(strings.TrimPrefix(ref, "hf.co/")), nil
	}
	return "", fmt.Errorf("download: cannot resolve reference %q (expected https://, file://, or hf.co/)", ref)
}

// resolveHF turns an HF shorthand into a resolve/main URL.
func resolveHF(path string) string {
	// hf.co/<org>/<repo>/<file>.gguf  →  direct file path
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 3 && strings.HasSuffix(parts[2], ".gguf") {
		return fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/%s", parts[0], parts[1], parts[2])
	}

	// hf.co/<org>/<repo>:<quant>
	if idx := strings.LastIndex(path, ":"); idx > 0 {
		repoPath := path[:idx]
		quant := path[idx+1:]
		repoParts := strings.SplitN(repoPath, "/", 2)
		if len(repoParts) == 2 {
			// Try to guess a filename. This is best-effort; the API handler
			// falls back to listing the repo if this 404s.
			filename := guessFilename(repoParts[1], quant)
			return fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/%s",
				repoParts[0], repoParts[1], filename)
		}
	}

	// hf.co/<org>/<repo>  — no quant specified, use Q4_K_M default
	repoParts := strings.SplitN(path, "/", 2)
	if len(repoParts) == 2 {
		filename := guessFilename(repoParts[1], "Q4_K_M")
		return fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/%s",
			repoParts[0], repoParts[1], filename)
	}

	// Fallback: just prepend the HF base.
	return "https://huggingface.co/" + path
}

// guessFilename tries to construct a GGUF filename from a repo name and quantization.
func guessFilename(repoName, quant string) string {
	// e.g. SmolLM2-135M-GGUF → SmolLM2-135M.Q4_K_M.gguf
	base := strings.TrimSuffix(repoName, "-GGUF")
	return base + "." + quant + ".gguf"
}

// Get downloads the resource at rawURL to destPath, reporting progress.
// It resumes partial downloads if a .partial file exists.
// Returns the hex SHA-256 of the downloaded content.
func Get(ctx context.Context, rawURL, destPath string, progress Progress) (digest string, err error) {
	// Handle file:// URIs.
	if strings.HasPrefix(rawURL, "file://") {
		src := strings.TrimPrefix(rawURL, "file://")
		return hashFile(src)
	}

	// Parse URL to validate.
	if _, err := url.Parse(rawURL); err != nil {
		return "", fmt.Errorf("download: invalid URL %q: %w", rawURL, err)
	}

	partialPath := destPath + ".partial"

	// Determine existing partial size for Range request.
	var startByte int64
	if fi, err := os.Stat(partialPath); err == nil {
		startByte = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: create request: %w", err)
	}
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: GET %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download: GET %q: HTTP %d", rawURL, resp.StatusCode)
	}

	// If server doesn't support range, restart from scratch.
	if resp.StatusCode == http.StatusOK && startByte > 0 {
		startByte = 0
		if err := os.Remove(partialPath); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if startByte > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(partialPath, flags, 0o644)
	if err != nil {
		return "", fmt.Errorf("download: open partial: %w", err)
	}

	total := resp.ContentLength + startByte
	downloaded := startByte

	h := sha256.New()
	// If resuming, hash what's already on disk.
	if startByte > 0 {
		existing, err := os.Open(partialPath)
		if err == nil {
			io.Copy(h, existing) //nolint:errcheck
			if err := existing.Close(); err != nil {
				return "", fmt.Errorf("download: close partial for hashing: %w", err)
			}
		}
	}

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			f.Close()
			return "", ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", fmt.Errorf("download: write: %w", werr)
			}
			h.Write(buf[:n])
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return "", fmt.Errorf("download: read: %w", err)
		}
	}
	f.Close()

	// Atomically rename to destination.
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(partialPath, destPath); err != nil {
		return "", fmt.Errorf("download: rename: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashFile returns the SHA-256 hex digest of a local file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
