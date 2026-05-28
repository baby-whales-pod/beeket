// Package main is the entry point for beeketd, the Beeket model server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/baby-whales-pod/beeket/internal/api"
	"github.com/baby-whales-pod/beeket/internal/config"
	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/libinstall"
	"github.com/baby-whales-pod/beeket/internal/models"
	"github.com/baby-whales-pod/beeket/internal/scheduler"
	"github.com/baby-whales-pod/beeket/internal/store"
	"github.com/baby-whales-pod/beeket/internal/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// flagValues holds the raw flag values bound by cobra before they are merged
// into a Config.
type flagValues struct {
	cfgPath string

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

	autoInstallLib    bool
	libVersion        string
	libUpgrade        bool
	libInstallTimeout time.Duration

	logLevel  string
	logFormat string
}

func newRootCmd() *cobra.Command {
	var fv flagValues

	cmd := &cobra.Command{
		Use:          "beeketd",
		Short:        "Beeket model server",
		Long:         "beeketd is the Beeket HTTP server for running GGUF language models locally.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, fv)
		},
	}

	cmd.Flags().StringVar(&fv.cfgPath, "config", "", "config file path (default: ~/.config/beeket/beeket.toml)")

	cmd.Flags().StringVar(&fv.host, "host", "127.0.0.1", "bind address")
	cmd.Flags().IntVar(&fv.port, "port", 11435, "listen port")

	cmd.Flags().StringVar(&fv.dataDir, "data-dir", "", "data directory (default $XDG_DATA_HOME/beeket)")
	cmd.Flags().StringVar(&fv.libDir, "lib-dir", "", "directory containing the llama.cpp shared library")

	cmd.Flags().StringVar(&fv.backend, "backend", "auto", "processor backend: auto|cpu|cuda|metal|vulkan|rocm")
	cmd.Flags().IntVar(&fv.gpuLayers, "gpu-layers", -1, "GPU layers to offload; -1 = all")
	cmd.Flags().IntVar(&fv.numParallel, "num-parallel", 1, "parallel inference slots per model")
	cmd.Flags().IntVar(&fv.maxLoaded, "max-loaded-models", 3, "max models held in memory")
	cmd.Flags().StringVar(&fv.keepAlive, "keep-alive", "5m", "model idle timeout")
	cmd.Flags().IntVar(&fv.contextSize, "context-size", 4096, "default context window in tokens")

	cmd.Flags().BoolVar(&fv.autoInstallLib, "auto-install-lib", false,
		"automatically download the llama.cpp shared library via yzma before starting")
	cmd.Flags().StringVar(&fv.libVersion, "lib-version", "",
		"llama.cpp version to install (default: latest); only used with --auto-install-lib")
	cmd.Flags().BoolVar(&fv.libUpgrade, "lib-upgrade", false,
		"force reinstall of the library even if already present; only used with --auto-install-lib")
	cmd.Flags().DurationVar(&fv.libInstallTimeout, "lib-install-timeout", 10*time.Minute,
		"maximum time to wait for the library download; only used with --auto-install-lib")

	cmd.Flags().StringVar(&fv.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	cmd.Flags().StringVar(&fv.logFormat, "log-format", "text", "log format: text|json")

	cmd.AddCommand(newVersionCmd())

	return cmd
}

func run(cmd *cobra.Command, fv flagValues) error {
	cfgPtr, err := config.Load(fv.cfgPath)
	if err != nil {
		return fmt.Errorf("beeketd: load config: %w", err)
	}
	cfg := *cfgPtr

	config.ApplyEnv(&cfg)
	applyFlags(&cfg, cmd, fv)

	if err := config.Validate(&cfg); err != nil {
		return err
	}

	log := newLogger(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	slog.Info("starting beeketd", "version", version.Version, "commit", version.Commit, "addr", cfg.Addr())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	go func() {
		sig := <-quit
		slog.Info("received signal, shutting down", "signal", sig.String())
		rootCancel()
	}()

	libDir := config.ResolveLibDir(&cfg)

	if cfg.Runtime.AutoInstallLib {
		slog.Info("--auto-install-lib enabled")

		installCtx, installCancel := context.WithTimeout(rootCtx, fv.libInstallTimeout)
		defer installCancel()

		resolvedBackend, err := libinstall.Ensure(installCtx, libinstall.Options{
			LibDir:  libDir,
			Backend: cfg.Runtime.Backend,
			Version: cfg.Runtime.LibVersion,
			Upgrade: cfg.Runtime.LibUpgrade,
			Logger:  log,
		})
		if err != nil {
			return fmt.Errorf("auto-install-lib: %w", err)
		}

		if err := os.Setenv("YZMA_LIB", libDir); err != nil {
			slog.Warn("failed to set YZMA_LIB", "err", err)
		}

		if cfg.Runtime.Backend == "auto" || cfg.Runtime.Backend == "" {
			cfg.Runtime.Backend = resolvedBackend
		}

		slog.Info("llama.cpp ready", "backend", cfg.Runtime.Backend, "lib_dir", libDir)
	}

	st, err := store.New(cfg.Paths.DataDir)
	if err != nil {
		return fmt.Errorf("beeketd: init store: %w", err)
	}

	eng, err := engine.New(libDir)
	if err != nil {
		return fmt.Errorf("beeketd: init engine: %w", err)
	}
	defer eng.Close()

	keepAlive, err := time.ParseDuration(cfg.Runtime.KeepAlive)
	if err != nil {
		return fmt.Errorf("beeketd: invalid keep-alive %q: %w", cfg.Runtime.KeepAlive, err)
	}

	mgr := models.New(st)
	sched := scheduler.New(eng, mgr, scheduler.Config{
		MaxLoaded:   cfg.Runtime.MaxLoaded,
		KeepAlive:   keepAlive,
		ContextSize: uint32(cfg.Runtime.ContextSize),
		NumParallel: cfg.Runtime.NumParallel,
		SamplerOpts: engine.DefaultSamplerOptions(),
	})

	handler := api.NewHandler(mgr, st, sched)
	srv := api.NewServer(handler)
	httpSrv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		slog.Info("beeketd listening", "addr", cfg.Addr())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
			rootCancel()
		}
	}()

	<-rootCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("beeketd: shutdown error", "err", err)
	}

	select {
	case err := <-srvErr:
		return fmt.Errorf("beeketd: listen error: %w", err)
	default:
	}

	slog.Info("beeketd: stopped")
	return nil
}

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

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print beeketd version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("beeketd %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildDate)
		},
	}
}
