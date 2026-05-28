// Package config defines the layered configuration for beeketd.
// Configuration sources (in increasing priority order):
//  1. Compiled-in defaults
//  2. Config file (~/.config/beeket/beeket.toml)
//  3. Environment variables (BEEKET_*)
//  4. Command-line flags
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config is the root configuration structure for beeketd.
type Config struct {
	Server  ServerConfig  `toml:"server"`
	Paths   PathsConfig   `toml:"paths"`
	Runtime RuntimeConfig `toml:"runtime"`
	Log     LogConfig     `toml:"log"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

// PathsConfig holds filesystem path settings.
type PathsConfig struct {
	// DataDir is the base data directory (XDG_DATA_HOME/beeket by default).
	DataDir string `toml:"data_dir"`
	// LibDir is the directory that contains the llama.cpp shared library.
	// Resolution order: --lib-dir flag → BEEKET_LIB_DIR → YZMA_LIB → DataDir/lib.
	LibDir string `toml:"lib_dir"`
}

// RuntimeConfig holds inference engine and auto-install settings.
type RuntimeConfig struct {
	// Backend selects the inference backend: auto | cpu | cuda | metal | vulkan | rocm.
	Backend string `toml:"backend"`

	// AutoInstallLib causes beeketd to invoke the yzma CLI to download the
	// llama.cpp shared library before starting the engine, if the library is
	// not already present.
	AutoInstallLib bool `toml:"auto_install_lib"`

	// LibVersion, when non-empty, is passed to `yzma install --version`.
	// Ignored unless AutoInstallLib is true.
	LibVersion string `toml:"lib_version"`

	// LibUpgrade forces a reinstall even when the library is already present.
	// Passed to `yzma install --upgrade`. Ignored unless AutoInstallLib is true.
	LibUpgrade bool `toml:"lib_upgrade"`

	GPULayers   int    `toml:"gpu_layers"`
	NumParallel int    `toml:"num_parallel"`
	MaxLoaded   int    `toml:"max_loaded"`
	KeepAlive   string `toml:"keep_alive"`
	ContextSize int    `toml:"context_size"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

// Defaults returns a Config populated with compiled-in defaults.
func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 11435,
		},
		Runtime: RuntimeConfig{
			Backend:     "auto",
			GPULayers:   -1,
			NumParallel: 1,
			MaxLoaded:   3,
			KeepAlive:   "5m",
			ContextSize: 4096,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// ResolveLibDir resolves the lib directory according to the priority order
// documented in spec §7.3 and the design doc §6:
//
//  1. --lib-dir flag / cfg.Paths.LibDir (already set by caller from flag)
//  2. BEEKET_LIB_DIR env
//  3. YZMA_LIB env (read-only)
//  4. <data-dir>/lib  (default when no other source is set)
func ResolveLibDir(cfg *Config) string {
	if cfg.Paths.LibDir != "" {
		return cfg.Paths.LibDir
	}
	if v := os.Getenv("BEEKET_LIB_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("YZMA_LIB"); v != "" {
		return v
	}
	return filepath.Join(resolveDataDir(cfg), "lib")
}

// resolveDataDir returns the effective data directory.
func resolveDataDir(cfg *Config) string {
	if cfg.Paths.DataDir != "" {
		return cfg.Paths.DataDir
	}
	if v := os.Getenv("BEEKET_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "beeket")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home is unavailable.
		return filepath.Join(".", ".local", "share", "beeket")
	}
	return filepath.Join(home, ".local", "share", "beeket")
}

// ApplyEnv overlays environment-variable overrides onto cfg.
// Flag-level overrides are applied by the cobra command after this.
func ApplyEnv(cfg *Config) {
	// Server
	if v := os.Getenv("BEEKET_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("BEEKET_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}

	// Paths — also reflect into cfg.Paths so callers don't need to re-read env.
	if v := os.Getenv("BEEKET_DATA_DIR"); v != "" {
		cfg.Paths.DataDir = v
	}
	if v := os.Getenv("BEEKET_LIB_DIR"); v != "" {
		cfg.Paths.LibDir = v
	}

	// Runtime
	if v := os.Getenv("BEEKET_BACKEND"); v != "" {
		cfg.Runtime.Backend = v
	}
	if v := os.Getenv("BEEKET_GPU_LAYERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Runtime.GPULayers = n
		}
	}
	if v := os.Getenv("BEEKET_NUM_PARALLEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Runtime.NumParallel = n
		}
	}
	if v := os.Getenv("BEEKET_MAX_LOADED_MODELS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Runtime.MaxLoaded = n
		}
	}
	if v := os.Getenv("BEEKET_KEEP_ALIVE"); v != "" {
		cfg.Runtime.KeepAlive = v
	}
	if v := os.Getenv("BEEKET_CONTEXT_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Runtime.ContextSize = n
		}
	}
	// Explicit false/0 must disable the flag; non-empty string that is neither
	// truthy nor falsy is intentionally ignored (preserves current value).
	if v := os.Getenv("BEEKET_AUTO_INSTALL_LIB"); v != "" {
		cfg.Runtime.AutoInstallLib = v == "true" || v == "1"
	}

	// Log
	if v := os.Getenv("BEEKET_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("BEEKET_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
}

// Validate returns an error if the configuration is self-contradictory.
func Validate(cfg *Config) error {
	validBackends := map[string]bool{
		"auto": true, "cpu": true, "cuda": true,
		"metal": true, "vulkan": true, "rocm": true,
	}
	if !validBackends[cfg.Runtime.Backend] {
		return fmt.Errorf("unknown backend %q; valid values: auto, cpu, cuda, metal, vulkan, rocm", cfg.Runtime.Backend)
	}
	return nil
}
