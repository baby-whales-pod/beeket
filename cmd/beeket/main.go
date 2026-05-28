// beeket — the Beeket CLI client.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/baby-whales-pod/beeket/internal/version"
	"github.com/baby-whales-pod/beeket/pkg/client"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var serverURL string

	root := &cobra.Command{
		Use:   "beeket",
		Short: "Beeket CLI — manage and run GGUF models",
	}
	root.PersistentFlags().StringVar(&serverURL, "server", "http://127.0.0.1:11435", "beeketd server URL")

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

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the beeketd server (delegates to beeketd binary)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Use `beeketd` directly to start the server.")
			return nil
		},
	}
}

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
			models, err := c.List(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tSIZE\tQUANT\tMODIFIED")
			for _, m := range models {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
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
				// Interactive mode: read from stdin.
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
			models, err := c.PS(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tSIZE\tLAST USED")
			for _, m := range models {
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					m.Name,
					humanBytes(m.Size),
					m.LastUsed.Format(time.RFC3339),
				)
			}
			return tw.Flush()
		},
	}
}

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
