// Package mcp exposes every service capability as an MCP tool. The adapters
// here only bind arguments and wrap results: parsing, validation and every
// rule live in package service.
package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AWDDude/loci/internal/service"
)

// instructions is what an MCP client shows the agent about Loci as a whole.
// Per-tool descriptions cover each tool; this covers how they fit together.
const instructions = `Loci is long-term memory organized around entities: people, organizations, projects, repositories, services, tools, places and concepts. Every memory is attached to at least one entity.

Recall by navigating: entity_search to find an entity, entity_get to see its edges to other entities and the titles of its memories, then memory_list or memory_get to read them. memory_search is a keyword fallback for when you do not know which entity to start from.

Search before you create. entity_search and memory_search each return a placeholder uuid, and entity_create and memory_create require the placeholder from the latest search of that kind. Reuse an existing result's id whenever it is the same thing. A "search again" error means another search or create happened in between: search again and retry.

Link types say how a memory relates to an entity: attribute (a fact about it), preference (how it wants things done), event (something that happened to or with it), decision (a choice made by it or about it), mention (a loose association).`

// NewServer returns an MCP server with every Loci tool registered.
func NewServer(svc *service.Service, version string) *server.MCPServer {
	s := server.NewMCPServer("loci", version,
		server.WithToolCapabilities(false),
		server.WithInstructions(instructions),
		server.WithRecovery(),
	)
	registerTools(s, svc)
	return s
}

// handle adapts a service method to an MCP tool handler: arguments are bound
// into A by their JSON names, and the result is returned as structured
// content (with its JSON as the text fallback). A service error becomes a
// tool error result, which is how MCP reports a failure the agent should read
// rather than a protocol fault.
func handle[A, R any](fn func(A) (R, error)) server.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		var args A
		if err := req.BindArguments(&args); err != nil {
			return mcpgo.NewToolResultError("invalid arguments: " + err.Error()), nil
		}
		res, err := fn(args)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultStructuredOnly(res), nil
	}
}

// idArgs is the argument of every tool that takes one uuid.
type idArgs struct {
	ID string `json:"id"`
}

// Deleted is the result of every delete tool: what was deleted, as the
// caller identified it.
type Deleted struct {
	Deleted any `json:"deleted"`
}
