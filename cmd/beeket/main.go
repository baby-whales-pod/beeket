// Package main is the entry point for the beeket CLI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/baby-whales-pod/beeket/internal/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "beeket",
		Short:        "Beeket CLI — manage models and run inference",
		SilenceUsage: true,
	}
	cmd.AddCommand(newVersionCmd())
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print beeket version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("beeket %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.BuildDate)
		},
	}
}
