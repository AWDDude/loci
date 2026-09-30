// Package cli exposes every MCP tool as a cobra command. A command is an MCP
// client: it connects to the daemon, makes one tools/call, and prints the
// result, as JSON with --json or formatted for a person otherwise. Nothing
// here implements a capability, which is what keeps the CLI and MCP surfaces
// at exact parity.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/config"
	"github.com/AWDDude/loci/internal/daemon"
)

// ToolAnnotation is the cobra annotation naming the MCP tool a command calls.
// The parity test reads it to match commands to tools.
const ToolAnnotation = "loci/tool"

// Caller calls MCP tools. The daemon connection implements it; tests
// substitute an in-process server.
type Caller interface {
	// Call invokes a tool and returns its structured result as JSON. A tool
	// error is returned as an error carrying the tool's message.
	Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)
	Close() error
}

// Env is what the commands share: the root's persistent flags and how to reach
// the tools.
type Env struct {
	// ConfigPath and JSON point at the root command's --config and --json.
	ConfigPath *string
	JSON       *bool
	Version    string
	// Connect returns a Caller. Nil means DialDaemon.
	Connect func(ctx context.Context) (Caller, error)
}

// AddCommands adds the entity, edge, memory and link command groups to root.
func AddCommands(root *cobra.Command, env *Env) {
	root.AddCommand(entityCmd(env), edgeCmd(env), memoryCmd(env), linkCmd(env))
}

// DialDaemon connects to the daemon for the configured database, starting one
// if needed, and completes the MCP handshake.
func (e *Env) DialDaemon(ctx context.Context) (Caller, error) {
	cfg, err := config.Load(*e.ConfigPath)
	if err != nil {
		return nil, err
	}
	conn, err := daemon.Dial(cfg, *e.ConfigPath, e.Version)
	if err != nil {
		return nil, err
	}
	return NewMCPCaller(ctx, client.NewClient(transport.NewIO(conn, conn, nil)))
}

// NewMCPCaller starts c and completes the MCP handshake.
func NewMCPCaller(ctx context.Context, c *client.Client) (Caller, error) {
	if err := c.Start(ctx); err != nil {
		c.Close()
		return nil, err
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "loci-cli", Version: "1"}
	if _, err := c.Initialize(ctx, init); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp handshake: %w", err)
	}
	return &mcpCaller{c: c}, nil
}

type mcpCaller struct{ c *client.Client }

func (m *mcpCaller) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = args
	res, err := m.c.CallTool(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.IsError {
		var parts []string
		for _, c := range res.Content {
			if tc, ok := c.(mcp.TextContent); ok {
				parts = append(parts, tc.Text)
			}
		}
		return nil, errors.New(strings.Join(parts, "\n"))
	}
	// Prefer the bytes as they came off the wire, so --json prints exactly
	// what an MCP client receives.
	if len(res.RawStructuredContent) > 0 {
		return res.RawStructuredContent, nil
	}
	return json.Marshal(res.StructuredContent)
}

func (m *mcpCaller) Close() error { return m.c.Close() }

// call invokes tool and prints its result: the JSON as is with --json,
// otherwise through human, which decodes the result into T.
func call[T any](cmd *cobra.Command, env *Env, tool string, args map[string]any, human func(io.Writer, T) error) error {
	connect := env.Connect
	if connect == nil {
		connect = env.DialDaemon
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	caller, err := connect(ctx)
	if err != nil {
		return err
	}
	defer caller.Close()

	raw, err := caller.Call(ctx, tool, args)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if *env.JSON {
		_, err := fmt.Fprintf(out, "%s\n", raw)
		return err
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("decoding %s result: %w", tool, err)
	}
	return human(out, v)
}

// leaf builds a command that calls tool. run receives the positional args.
func leaf(use, short, tool string, nargs int, run func(cmd *cobra.Command, args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Args:        cobra.ExactArgs(nargs),
		Annotations: map[string]string{ToolAnnotation: tool},
		RunE:        run,
	}
}

// setIfChanged adds a flag's value to args only when the user gave the flag,
// so an update sends exactly the fields being changed.
func setIfChanged(cmd *cobra.Command, args map[string]any, flag, key string, value any) {
	if cmd.Flags().Changed(flag) {
		args[key] = value
	}
}

// readContent returns s, or all of stdin when s is "-", so multi-line content
// can be piped in.
func readContent(cmd *cobra.Command, s string) (string, error) {
	if s != "-" {
		return s, nil
	}
	in := cmd.InOrStdin()
	if in == nil {
		in = os.Stdin
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("reading content from stdin: %w", err)
	}
	return string(data), nil
}
