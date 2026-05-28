// beeketd — the Beeket server daemon.
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

	"github.com/baby-whales-pod/beeket/internal/api"
	"github.com/baby-whales-pod/beeket/internal/config"
	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/models"
	"github.com/baby-whales-pod/beeket/internal/scheduler"
	"github.com/baby-whales-pod/beeket/internal/store"
	"github.com/baby-whales-pod/beeket/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var cfgPath string
	var host string
	var port int
	var libDir string
	var logLevel string
	var logFormat string

	cmd := &cobra.Command{
		Use:   "beeketd",
		Short: "Beeket model server",
		Long:  "beeketd is the Beeket HTTP server for running GGUF language models locally.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cfgPath, host, port, libDir, logLevel, logFormat)
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "config file path (default: ~/.config/beeket/beeket.toml)")
	cmd.Flags().StringVar(&host, "host", "", "bind host (overrides config)")
	cmd.Flags().IntVar(&port, "port", 0, "bind port (overrides config)")
	cmd.Flags().StringVar(&libDir, "lib-dir", "", "llama.cpp library directory (overrides config)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level: debug|info|warn|error")
	cmd.Flags().StringVar(&logFormat, "log-format", "", "log format: text|json")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
	cmd.AddCommand(versionCmd)

	return cmd
}

func run(cfgPath, host string, port int, libDir, logLevel, logFormat string) error {
	// Load config.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("beeketd: load config: %w", err)
	}
	// Apply flag overrides.
	if host != "" {
		cfg.Server.Host = host
	}
	if port != 0 {
		cfg.Server.Port = port
	}
	if libDir != "" {
		cfg.Paths.LibDir = libDir
	}
	if logLevel != "" {
		cfg.Log.Level = logLevel
	}
	if logFormat != "" {
		cfg.Log.Format = logFormat
	}

	// Configure logger.
	setupLogger(cfg.Log.Level, cfg.Log.Format)

	slog.Info("starting beeketd", "version", version.Version, "addr", cfg.Addr())

	// Initialise store.
	st, err := store.New(cfg.Paths.DataDir)
	if err != nil {
		return fmt.Errorf("beeketd: init store: %w", err)
	}

	// Initialise engine.
	eng, err := engine.New(cfg.Paths.LibDir)
	if err != nil {
		return fmt.Errorf("beeketd: init engine: %w", err)
	}
	defer eng.Close()

	// Model manager.
	mgr := models.New(st)

	// Scheduler.
	sched := scheduler.New(eng, mgr, scheduler.Config{
		MaxLoaded:   cfg.Runtime.MaxLoaded,
		KeepAlive:   cfg.Runtime.KeepAlive,
		ContextSize: uint32(cfg.Runtime.ContextSize),
		NumParallel: cfg.Runtime.NumParallel,
		SamplerOpts: engine.DefaultSamplerOptions(),
	})

	// HTTP server.
	handler := api.NewHandler(mgr, st, sched)
	srv := api.NewServer(handler)
	httpSrv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // streaming responses — no write timeout
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("beeketd listening", "addr", cfg.Addr())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("beeketd: listen error", "err", err)
			quit <- syscall.SIGTERM
		}
	}()

	<-quit
	slog.Info("beeketd: shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("beeketd: shutdown error", "err", err)
	}
	slog.Info("beeketd: stopped")
	return nil
}

func setupLogger(level, format string) {
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
	slog.SetDefault(slog.New(h))
}
