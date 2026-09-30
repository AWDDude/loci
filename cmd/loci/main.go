// Command loci is an entity-oriented persistent memory store for coding
// agents, served over MCP and driven from the same binary's CLI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is the build version, injected by `task build` via
// -ldflags "-X main.version=...". A bare `go build` reports "dev".
var version = "dev"

// globalFlags are the persistent flags every command inherits.
type globalFlags struct {
	config string
	json   bool
}

func newRootCmd() *cobra.Command {
	var flags globalFlags
	root := &cobra.Command{
		Use:   "loci",
		Short: "Entity-oriented persistent memory for coding agents",
		// Errors are printed once by main, and a usage dump would bury them.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&flags.config, "config", "", "config file (default $XDG_CONFIG_HOME/loci/config.yaml)")
	root.PersistentFlags().BoolVar(&flags.json, "json", false, "print JSON instead of human-readable output")

	root.AddCommand(
		newVersionCmd(&flags),
		newServeCmd(&flags),
		newDaemonCmd(&flags),
	)
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "loci: %v\n", err)
		os.Exit(1)
	}
}
