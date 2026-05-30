// Package config defines the layered configuration for beeket.
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
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration structure for beeket.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Paths    PathsConfig    `toml:"paths"`
	Runtime  RuntimeConfig  `toml:"runtime"`
	Download DownloadConfig `toml:"download"`
	Log      LogConfig      `toml:"log"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host    string   `toml:"host"`
	Port    int      `toml:"port"`
	Origins []string `toml:"origins"`
}

// PathsConfig holds filesystem path settings.
type PathsConfig struct {
	DataDir string `toml:"data_dir"`
	LibDir  string `toml:"lib_dir"`
}

// RuntimeConfig holds inference engine and auto-install settings.
type RuntimeConfig struct {
	Backend        string `toml:"backend"`
	AutoInstallLib bool   `toml:"auto_install_lib"`
	LibVersion     string `toml:"lib_version"`
	LibUpgrade     bool   `toml:"lib_upgrade"`

	GPULayers   int    `toml:"gpu_layers"`
	NumParallel int    `toml:"num_parallel"`
	MaxLoaded   int    `toml:"max_loaded"`
	KeepAlive   string `toml:"keep_alive"`
	ContextSize int    `toml:"context_size"`

	// Metrics configuration.
	MetricsEnabled bool   `toml:"metrics_enabled"`
	MetricsBind    string `toml:"metrics_bind"`
}

// DownloadConfig holds download manager settings.
type DownloadConfig struct {
	Concurrency int `toml:"concurrency"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

// Defaults returns a Config populated with compiled-in defaults.
func Defaults() Config {
	dataDir := defaultDataDir()
	return Config{
		Server: ServerConfig{
			Host:    "127.0.0.1",
			Port:    11435,
			Origins: []string{"http://localhost:11435", "http://127.0.0.1:11435"},
		},
		Paths: PathsConfig{
			DataDir: dataDir,
			LibDir:  "",
		},
		Runtime: RuntimeConfig{
			Backend:        "auto",
			GPULayers:      -1,
			NumParallel:    1,
			MaxLoaded:      3,
			KeepAlive:      "5m",
			ContextSize:    4096,
			MetricsEnabled: true,
		},
		Download: DownloadConfig{
			Concurrency: 4,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Load reads and merges config from the given TOML file path into defaults.
// Missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	if path == "" {
		path = defaultConfigPath()
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &cfg, nil
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	return &cfg, nil
}

// Addr returns the host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// ResolveLibDir resolves the lib directory according to the priority order:
//  1. --lib-dir flag / cfg.Paths.LibDir
//  2. BEEKET_LIB_DIR env
//  3. YZMA_LIB env
//  4. <data-dir>/lib
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

func resolveDataDir(cfg *Config) string {
	if cfg.Paths.DataDir != "" {
		return cfg.Paths.DataDir
	}
	if v := os.Getenv("BEEKET_DATA_DIR"); v != "" {
		return v
	}
	return defaultDataDir()
}

// ApplyEnv overlays environment variables (BEEKET_*) onto cfg.
// It is called after loading the config file so env vars take precedence.
func ApplyEnv(cfg *Config) {
	if v := os.Getenv("BEEKET_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("BEEKET_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}

	if v := os.Getenv("BEEKET_DATA_DIR"); v != "" {
		cfg.Paths.DataDir = v
	}
	if v := os.Getenv("BEEKET_LIB_DIR"); v != "" {
		cfg.Paths.LibDir = v
	}

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
	if v := os.Getenv("BEEKET_AUTO_INSTALL_LIB"); v != "" {
		cfg.Runtime.AutoInstallLib = v == "true" || v == "1"
	}
	if v := os.Getenv("BEEKET_METRICS_ENABLED"); v != "" {
		cfg.Runtime.MetricsEnabled = v == "true" || v == "1"
	}
	if v := os.Getenv("BEEKET_METRICS_BIND"); v != "" {
		cfg.Runtime.MetricsBind = v
	}

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
	if _, err := time.ParseDuration(cfg.Runtime.KeepAlive); err != nil {
		return fmt.Errorf("invalid keep_alive %q: %w", cfg.Runtime.KeepAlive, err)
	}
	return nil
}

func defaultDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "beeket")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "share", "beeket")
	}
	return filepath.Join(home, ".local", "share", "beeket")
}

func defaultConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "beeket", "beeket.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "beeket", "beeket.toml")
}
