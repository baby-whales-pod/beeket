// Package main is the entry point for beeketd, the Beeket model server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/baby-whales-pod/beeket/internal/config"
	"github.com/baby-whales-pod/beeket/internal/libinstall"
	"github.com/baby-whales-pod/beeket/internal/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// flagValues holds the raw flag values bound by cobra before they are merged
// into a Config. Using a dedicated struct (rather than mutating cfg directly)
// keeps the zero-value semantics clean.
type flagValues struct {
	host string
	port int

	dataDir string
	libDir  string

	backend     string
	gpuLayers   int
	numParallel int
	maxLoaded   int
	keepAlive   string
	contextSize int

	// auto-install-lib flags
	autoInstallLib bool
	libVersion     string
	libUpgrade     bool

	logLevel  string
	logFormat string
}

func newRootCmd() *cobra.Command {
	var fv flagValues

	cmd := &cobra.Command{
		Use:   "beeketd",
		Short: "Beeket model server",
		Long: `beeketd is the Beeket inference server.

It loads GGUF models via the Yzma library and serves an Ollama-compatible
REST API on 127.0.0.1:11435 by default.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, fv)
		},
	}

	// Server flags
	cmd.Flags().StringVar(&fv.host, "host", "", "bind address (default 127.0.0.1)")
	cmd.Flags().IntVar(&fv.port, "port", 0, "listen port (default 11435)")

	// Path flags
	cmd.Flags().StringVar(&fv.dataDir, "data-dir", "", "data directory (default $XDG_DATA_HOME/beeket)")
	cmd.Flags().StringVar(&fv.libDir, "lib-dir", "", "directory containing the llama.cpp shared library")

	// Runtime flags
	cmd.Flags().StringVar(&fv.backend, "backend", "", "processor backend: auto|cpu|cuda|metal|vulkan|rocm (default auto)")
	cmd.Flags().IntVar(&fv.gpuLayers, "gpu-layers", 0, "GPU layers to offload; -1 = all (default -1)")
	cmd.Flags().IntVar(&fv.numParallel, "num-parallel", 0, "parallel inference slots per model (default 1)")
	cmd.Flags().IntVar(&fv.maxLoaded, "max-loaded-models", 0, "max models held in memory (default 3)")
	cmd.Flags().StringVar(&fv.keepAlive, "keep-alive", "", "model idle timeout (default 5m)")
	cmd.Flags().IntVar(&fv.contextSize, "context-size", 0, "default context window in tokens (default 4096)")

	// auto-install-lib flags
	cmd.Flags().BoolVar(&fv.autoInstallLib, "auto-install-lib", false,
		"automatically download the llama.cpp shared library via yzma before starting")
	cmd.Flags().StringVar(&fv.libVersion, "lib-version", "",
		"llama.cpp version to install (default: latest); only used with --auto-install-lib")
	cmd.Flags().BoolVar(&fv.libUpgrade, "lib-upgrade", false,
		"force reinstall of the library even if already present; only used with --auto-install-lib")

	// Log flags
	cmd.Flags().StringVar(&fv.logLevel, "log-level", "", "log level: debug|info|warn|error (default info)")
	cmd.Flags().StringVar(&fv.logFormat, "log-format", "", "log format: text|json (default text)")

	// version subcommand
	cmd.AddCommand(newVersionCmd())

	return cmd
}

// run is the main execution path for beeketd.
func run(cmd *cobra.Command, fv flagValues) error {
	// 1. Build config from defaults → env → flags.
	cfg := config.Defaults()
	config.ApplyEnv(&cfg)
	applyFlags(&cfg, cmd, fv)

	if err := config.Validate(&cfg); err != nil {
		return err
	}

	// 2. Set up logger.
	log := newLogger(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	log.Info("beeketd starting", "version", version.Version, "commit", version.Commit)

	// 3. Resolve lib dir (used by auto-install and engine init).
	libDir := config.ResolveLibDir(&cfg)

	// 4. Auto-install the llama.cpp shared library if requested.
	if cfg.Runtime.AutoInstallLib {
		log.Info("--auto-install-lib enabled")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		resolvedBackend, err := libinstall.Ensure(ctx, libinstall.Options{
			LibDir:  libDir,
			Backend: cfg.Runtime.Backend,
			Version: cfg.Runtime.LibVersion,
			Upgrade: cfg.Runtime.LibUpgrade,
			Logger:  log,
		})
		if err != nil {
			return fmt.Errorf("auto-install-lib: %w", err)
		}

		// Propagate the resolved lib dir into the process environment so the
		// engine loader picks it up through the standard YZMA_LIB variable.
		// This is process-local and does not mutate the user's shell.
		if err := os.Setenv("YZMA_LIB", libDir); err != nil {
			log.Warn("failed to set YZMA_LIB", "err", err)
		}

		// Update config so subsequent code (e.g. engine init) sees the
		// resolved backend rather than "auto".
		if cfg.Runtime.Backend == "auto" || cfg.Runtime.Backend == "" {
			cfg.Runtime.Backend = resolvedBackend
		}

		log.Info("llama.cpp ready",
			"backend", cfg.Runtime.Backend,
			"lib_dir", libDir)
	}

	// 5. TODO(v0.1): Initialise the engine, model manager, scheduler, and
	//    HTTP server here once those packages exist. For now we print a startup
	//    banner so the binary is functional enough to test the flag wiring.
	log.Info("listening (stub — engine not yet implemented)",
		"host", cfg.Server.Host,
		"port", cfg.Server.Port)

	// Wait for SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info("shutting down", "signal", sig.String())

	return nil
}

// applyFlags overlays the cobra flag values onto cfg, skipping any flag that
// was not explicitly provided by the user (so defaults and env vars win).
func applyFlags(cfg *config.Config, cmd *cobra.Command, fv flagValues) {
	if cmd.Flags().Changed("host") {
		cfg.Server.Host = fv.host
	}
	if cmd.Flags().Changed("port") {
		cfg.Server.Port = fv.port
	}
	if cmd.Flags().Changed("data-dir") {
		cfg.Paths.DataDir = fv.dataDir
	}
	if cmd.Flags().Changed("lib-dir") {
		cfg.Paths.LibDir = fv.libDir
	}
	if cmd.Flags().Changed("backend") {
		cfg.Runtime.Backend = fv.backend
	}
	if cmd.Flags().Changed("gpu-layers") {
		cfg.Runtime.GPULayers = fv.gpuLayers
	}
	if cmd.Flags().Changed("num-parallel") {
		cfg.Runtime.NumParallel = fv.numParallel
	}
	if cmd.Flags().Changed("max-loaded-models") {
		cfg.Runtime.MaxLoaded = fv.maxLoaded
	}
	if cmd.Flags().Changed("keep-alive") {
		cfg.Runtime.KeepAlive = fv.keepAlive
	}
	if cmd.Flags().Changed("context-size") {
		cfg.Runtime.ContextSize = fv.contextSize
	}
	if cmd.Flags().Changed("auto-install-lib") {
		cfg.Runtime.AutoInstallLib = fv.autoInstallLib
	}
	if cmd.Flags().Changed("lib-version") {
		cfg.Runtime.LibVersion = fv.libVersion
	}
	if cmd.Flags().Changed("lib-upgrade") {
		cfg.Runtime.LibUpgrade = fv.libUpgrade
	}
	if cmd.Flags().Changed("log-level") {
		cfg.Log.Level = fv.logLevel
	}
	if cmd.Flags().Changed("log-format") {
		cfg.Log.Format = fv.logFormat
	}
}

// newLogger constructs a slog.Logger from level and format strings.
func newLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h)
}

// newVersionCmd returns the `beeketd version` subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print beeketd version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("beeketd %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.BuildDate)
		},
	}
}
