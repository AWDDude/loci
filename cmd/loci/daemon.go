package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/config"
	"github.com/AWDDude/loci/internal/daemon"
	"github.com/AWDDude/loci/internal/mcp"
	"github.com/AWDDude/loci/internal/service"
	"github.com/AWDDude/loci/internal/store"
)

func newServeCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve MCP on stdio (what an MCP client runs)",
		Long: `Serve MCP on stdin and stdout for an MCP client such as a coding agent.

The process is a pipe to the shared daemon that owns the database, starting
one if none is running, so any number of sessions share one store.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(flags.config)
			if err != nil {
				return err
			}
			return daemon.Proxy(cfg, flags.config, version, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func newDaemonCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the daemon that owns the database, in the foreground",
		Long: `Run the daemon that owns the database, in the foreground.

Clients start it on demand, so running it by hand is only useful for
debugging. It exits after 10 minutes with no clients attached.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(flags.config)
			if err != nil {
				return err
			}
			return runDaemon(cfg)
		},
	}
	cmd.AddCommand(newDaemonStatusCmd(flags), newDaemonStopCmd(flags))
	return cmd
}

func runDaemon(cfg config.Config) error {
	// The log is a file read after the fact, usually to explain why a
	// client's dial timed out, so it gets timestamps.
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("loci-daemon: ")

	// Cancelling the context closes the listener and ends attached sessions,
	// so the socket and pid file are cleaned up on `loci daemon stop`.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	newMCP := func() (*mcpserver.MCPServer, func(), error) {
		st, err := store.Open(cfg.DB.Path)
		if err != nil {
			return nil, nil, err
		}
		return mcp.NewServer(service.New(st), version), func() { st.Close() }, nil
	}
	if err := daemon.Serve(ctx, cfg, version, newMCP, daemon.DefaultIdleTimeout); err != nil {
		log.Printf("%v", err)
		return err
	}
	return nil
}

func newDaemonStatusCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether a daemon is running for the configured database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(flags.config)
			if err != nil {
				return err
			}
			st := daemon.Query(cfg)
			out := cmd.OutOrStdout()
			if flags.json {
				return json.NewEncoder(out).Encode(st)
			}
			if !st.Running {
				_, err := fmt.Fprintf(out, "No loci daemon is running for %s.\n", st.Database)
				return err
			}
			pid := ""
			if st.PID != 0 {
				pid = fmt.Sprintf(" (pid %d)", st.PID)
			}
			_, err = fmt.Fprintf(out, "loci daemon %s serving %s on %s%s\n", st.Version, st.Database, st.Socket, pid)
			return err
		},
	}
}

func newDaemonStopCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon; the next client starts a fresh one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(flags.config)
			if err != nil {
				return err
			}
			wasRunning, err := daemon.Stop(cfg)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flags.json {
				return json.NewEncoder(out).Encode(map[string]bool{"stopped": wasRunning})
			}
			msg := "Stopped the loci daemon."
			if !wasRunning {
				msg = "No loci daemon was running."
			}
			_, err = fmt.Fprintln(out, msg)
			return err
		},
	}
}
