// Package config provides layered configuration for Beeket.
// Layers (later overrides earlier): compiled-in defaults → TOML file → env vars → CLI flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration structure.
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

// RuntimeConfig holds inference runtime settings.
type RuntimeConfig struct {
	Backend     string        `toml:"backend"`
	GPULayers   int           `toml:"gpu_layers"`
	NumParallel int           `toml:"num_parallel"`
	MaxLoaded   int           `toml:"max_loaded"`
	KeepAlive   time.Duration `toml:"keep_alive"`
	ContextSize int           `toml:"context_size"`
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
func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host:    "127.0.0.1",
			Port:    11435,
			Origins: []string{"http://localhost:11435", "http://127.0.0.1:11435"},
		},
		Paths: PathsConfig{
			DataDir: defaultDataDir(),
		},
		Runtime: RuntimeConfig{
			Backend:     "auto",
			GPULayers:   -1,
			NumParallel: 1,
			MaxLoaded:   3,
			KeepAlive:   5 * time.Minute,
			ContextSize: 4096,
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
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	return cfg, nil
}

// Addr returns the host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// defaultDataDir returns the XDG_DATA_HOME/beeket path.
func defaultDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "beeket")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "beeket")
}

// defaultConfigPath returns the XDG_CONFIG_HOME/beeket/beeket.toml path.
func defaultConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "beeket", "beeket.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "beeket", "beeket.toml")
}
