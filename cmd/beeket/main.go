// beeket — the single Beeket binary: CLI client and model server.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
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
	"github.com/baby-whales-pod/beeket/pkg/client"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var serverURL string

	root := &cobra.Command{
		Use:          "beeket",
		Short:        "Beeket — manage and run GGUF models",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&serverURL, "server", "http://127.0.0.1:11435", "server URL for client commands")

	newClient := func() *client.Client {
		return client.New(serverURL)
	}

	root.AddCommand(
		versionCmd(),
		serveCmd(),
		pullCmd(newClient),
		listCmd(newClient),
		showCmd(newClient),
		rmCmd(newClient),
		runCmd(newClient),
		psCmd(newClient),
	)
	return root
}

// ---------------------------------------------------------------------------
// version
// ---------------------------------------------------------------------------

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

// ---------------------------------------------------------------------------
// serve — starts the Beeket HTTP server
// ---------------------------------------------------------------------------

// serveFlagValues holds cobra flag bindings for the serve subcommand.
type serveFlagValues struct {
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

func serveCmd() *cobra.Command {
	var fv serveFlagValues

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Beeket model server",
		Long: `Start the Beeket HTTP server for running GGUF language models locally.

Binds to 127.0.0.1:11435 by default and serves an Ollama-compatible REST API.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, fv)
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

	return cmd
}

func runServe(cmd *cobra.Command, fv serveFlagValues) error {
	cfgPtr, err := config.Load(fv.cfgPath)
	if err != nil {
		return fmt.Errorf("beeket serve: load config: %w", err)
	}
	cfg := *cfgPtr

	config.ApplyEnv(&cfg)
	applyServeFlags(&cfg, cmd, fv)

	if err := config.Validate(&cfg); err != nil {
		return err
	}

	log := newLogger(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	slog.Info("starting beeket serve", "version", version.Version, "commit", version.Commit, "addr", cfg.Addr())

	// Signal handling set up early so CTRL+C works during library download.
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
		return fmt.Errorf("beeket serve: init store: %w", err)
	}

	eng, err := engine.New(libDir)
	if err != nil {
		return fmt.Errorf("beeket serve: init engine: %w", err)
	}
	defer eng.Close()

	keepAlive, err := time.ParseDuration(cfg.Runtime.KeepAlive)
	if err != nil {
		return fmt.Errorf("beeket serve: invalid keep-alive %q: %w", cfg.Runtime.KeepAlive, err)
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
		slog.Info("beeket serve: listening", "addr", cfg.Addr())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
			rootCancel()
		}
	}()

	<-rootCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("beeket serve: shutdown error", "err", err)
	}

	select {
	case err := <-srvErr:
		return fmt.Errorf("beeket serve: listen error: %w", err)
	default:
	}

	slog.Info("beeket serve: stopped")
	return nil
}

// applyServeFlags overlays explicit cobra flag values onto cfg.
// Uses Changed() so only flags explicitly provided by the user override env/config.
func applyServeFlags(cfg *config.Config, cmd *cobra.Command, fv serveFlagValues) {
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

// ---------------------------------------------------------------------------
// Client subcommands (pull, list, show, rm, run, ps)
// ---------------------------------------------------------------------------

func pullCmd(newClient func() *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "pull <model>",
		Short: "Download a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			return c.Pull(cmd.Context(), args[0], func(status, digest string, total, completed int64) {
				if total > 0 {
					pct := float64(completed) / float64(total) * 100
					fmt.Printf("\r%s  %.1f%%", status, pct)
				} else {
					fmt.Printf("\r%s", status)
				}
				if status == "success" {
					fmt.Println()
				}
			})
		},
	}
}

func listCmd(newClient func() *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed models",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			mods, err := c.List(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tSIZE\tQUANT\tMODIFIED") //nolint:errcheck // stdout write errors are unrecoverable in a CLI
			for _, m := range mods {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", //nolint:errcheck
					m.Name,
					humanBytes(m.Size),
					m.Details.QuantizationLevel,
					m.ModifiedAt.Format(time.RFC3339),
				)
			}
			return tw.Flush()
		},
	}
}

func showCmd(newClient func() *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "show <model>",
		Short: "Show model details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			info, err := c.Show(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(info)
		},
	}
}

func rmCmd(newClient func() *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <model>",
		Short: "Remove a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("deleted %s\n", args[0])
			return nil
		},
	}
}

func runCmd(newClient func() *client.Client) *cobra.Command {
	var prompt string
	var stream bool

	cmd := &cobra.Command{
		Use:   "run <model>",
		Short: "Run a one-shot generation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if prompt == "" {
				scanner := bufio.NewScanner(os.Stdin)
				fmt.Print("> ")
				if scanner.Scan() {
					prompt = scanner.Text()
				}
			}
			if stream {
				return c.Generate(cmd.Context(), args[0], prompt, func(piece string) {
					fmt.Print(piece)
				})
			}
			result, err := c.GenerateSync(cmd.Context(), args[0], prompt)
			if err != nil {
				return err
			}
			fmt.Println(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "prompt text")
	cmd.Flags().BoolVar(&stream, "stream", true, "stream output")
	return cmd
}

func psCmd(newClient func() *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "ps",
		Short: "List loaded models",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			mods, err := c.PS(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tSIZE\tLAST USED") //nolint:errcheck // stdout write errors are unrecoverable in a CLI
			for _, m := range mods {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", //nolint:errcheck
					m.Name,
					humanBytes(m.Size),
					m.LastUsed.Format(time.RFC3339),
				)
			}
			return tw.Flush()
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
