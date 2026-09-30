package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/cli"
	"github.com/AWDDude/loci/internal/mcp"
	"github.com/AWDDude/loci/internal/service"
	"github.com/AWDDude/loci/internal/store"
)

// notTools are the runnable commands that deliberately have no MCP tool: they
// manage the process rather than the memory store. Anything else runnable
// must call exactly one tool.
var notTools = []string{
	"loci daemon",
	"loci daemon status",
	"loci daemon stop",
	"loci serve",
	"loci version",
}

// TestCLIAndMCPParity enforces invariant 3: every MCP tool has exactly one
// CLI command and every CLI command (bar notTools) calls exactly one tool.
func TestCLIAndMCPParity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "loci.bbolt"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var tools []string
	for name := range mcp.NewServer(service.New(st), "test").ListTools() {
		tools = append(tools, name)
	}
	slices.Sort(tools)

	var commandTools, stray []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if tool, ok := c.Annotations[cli.ToolAnnotation]; ok {
			commandTools = append(commandTools, tool)
		} else if c.Runnable() && !slices.Contains(notTools, c.CommandPath()) {
			stray = append(stray, c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	slices.Sort(commandTools)

	if len(stray) > 0 {
		t.Errorf("commands that call no tool and are not in notTools: %v", stray)
	}
	if !slices.Equal(commandTools, tools) {
		t.Errorf("CLI commands call %v\nMCP server has      %v", commandTools, tools)
	}
	if len(slices.Compact(slices.Clone(commandTools))) != len(commandTools) {
		t.Errorf("a tool is called by more than one command: %v", commandTools)
	}
}
