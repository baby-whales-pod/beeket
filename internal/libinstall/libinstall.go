// Package libinstall manages downloading the llama.cpp shared library via
// the yzma CLI. It is only invoked when --auto-install-lib is set.
package libinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Options configure a library installation.
type Options struct {
	// LibDir is the target directory (must be non-empty, already resolved).
	LibDir string
	// Backend is "auto" or an explicit processor name (cpu|cuda|metal|vulkan|rocm).
	// When "auto", DetectBackend() is called to resolve it.
	Backend string
	// Version is passed to `yzma install --version`. Empty means latest.
	Version string
	// Upgrade forces reinstall even when the library is already present.
	// Passed as `yzma install --upgrade`.
	Upgrade bool
	// Logger is used for structured log output. If nil, slog.Default() is used.
	Logger *slog.Logger
	// Stdout / Stderr allow redirecting yzma's output (used in tests).
	Stdout io.Writer
	Stderr io.Writer
}

// libNames is the ordered list of shared-library filenames to look for.
var libNames = []string{
	"libllama.so",    // Linux
	"libllama.dylib", // macOS
	"llama.dll",      // Windows
	"libllama.so.0",  // some Linux distros versioned
}

// Ensure makes sure a llama.cpp shared library exists in opts.LibDir,
// downloading it via the yzma CLI if needed. It returns the detected/used
// processor backend so callers can log or record it.
func Ensure(ctx context.Context, opts Options) (backend string, err error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	if opts.LibDir == "" {
		return "", errors.New("libinstall: LibDir must not be empty")
	}

	// Resolve backend before anything else so we log it early.
	backend = opts.Backend
	if backend == "" || backend == "auto" {
		backend = DetectBackend()
		log.Info("detected backend", "backend", backend,
			"goos", runtime.GOOS, "goarch", runtime.GOARCH)
	} else {
		log.Info("using explicit backend", "backend", backend)
	}

	log.Info("lib dir", "path", opts.LibDir)

	// Fast pre-check: is the library already installed?
	if !opts.Upgrade {
		if existing, found := findLibrary(opts.LibDir); found {
			log.Info("llama.cpp library already installed, skipping",
				"file", existing)
			return backend, nil
		}
	}

	// Locate the yzma binary.
	yzmaPath, err := resolveYzma()
	if err != nil {
		return "", err
	}
	log.Info("found yzma", "path", yzmaPath)

	// Create lib dir if it doesn't exist.
	if err := os.MkdirAll(opts.LibDir, 0755); err != nil {
		return "", fmt.Errorf("libinstall: create lib dir %q: %w", opts.LibDir, err)
	}

	// Build yzma install command.
	args := []string{"install", "--lib", opts.LibDir, "--processor", backend}
	if opts.Version != "" {
		args = append(args, "--version", opts.Version)
	}
	if opts.Upgrade {
		args = append(args, "--upgrade")
	}

	log.Info("running", "cmd", yzmaPath+" "+strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, yzmaPath, args...)

	// Propagate YZMA_LIB to child so yzma's own defaults align with ours.
	cmd.Env = append(os.Environ(), "YZMA_LIB="+opts.LibDir)

	// Wire up output streams.
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("libinstall: yzma install failed: %w", err)
	}

	log.Info("llama.cpp library installed successfully", "backend", backend, "dir", opts.LibDir)
	return backend, nil
}

// findLibrary checks whether any of the expected shared library filenames
// exist in dir. Returns the found path and true, or ("", false).
func findLibrary(dir string) (string, bool) {
	for _, name := range libNames {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// resolveYzma locates the yzma CLI binary using the following priority:
//  1. exec.LookPath("yzma")  — any entry on PATH
//  2. $GOBIN/yzma
//  3. $(go env GOPATH)/bin/yzma
//
// If none is found, it returns a descriptive error with install instructions.
func resolveYzma() (string, error) {
	// 1. PATH
	if p, err := exec.LookPath("yzma"); err == nil {
		return p, nil
	}

	// 2. $GOBIN
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		candidate := filepath.Join(gobin, yzmaBinary())
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 3. $(go env GOPATH)/bin — resolve GOPATH ourselves to avoid shelling out.
	if gopath := goPath(); gopath != "" {
		candidate := filepath.Join(gopath, "bin", yzmaBinary())
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(`beeketd: --auto-install-lib requires the 'yzma' CLI on PATH.

  Install it once with:
    go install github.com/hybridgroup/yzma@latest

  Then re-run beeketd, or pre-install the library yourself:
    yzma install --lib <dir> --processor <cpu|cuda|metal|vulkan|rocm>`)
}

// goPath returns GOPATH (from env, then the Go default of $HOME/go).
func goPath() string {
	if v := os.Getenv("GOPATH"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go")
}

// yzmaBinary returns the platform-specific binary name for yzma.
func yzmaBinary() string {
	if runtime.GOOS == "windows" {
		return "yzma.exe"
	}
	return "yzma"
}
