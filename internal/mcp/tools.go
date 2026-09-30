package mcp

import (
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/service"
)

// names converts a closed type set to the strings a schema enum lists, so the
// enums are generated from the same lists the service validates against.
func names[T ~string](ts []T) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

// Argument fragments shared by several tools.
var (
	entityTypeEnum = mcpgo.Enum(names(model.EntityTypes)...)
	linkTypeEnum   = mcpgo.Enum(names(model.LinkTypes)...)
	edgeTypeEnum   = mcpgo.Enum(model.EdgeTypeNames()...)

	limitArg = mcpgo.WithNumber("limit",
		mcpgo.Description("Maximum results. Omit or pass 0 for the default of 20."),
		mcpgo.Min(0),
	)
	aliasesDescription = "Other names the entity answers to, e.g. nicknames or former names. Casing is kept; matching ignores case."
)

func idArg(what string) mcpgo.ToolOption {
	return mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("uuid of the "+what))
}

func placeholderArg(kind, search string) mcpgo.ToolOption {
	return mcpgo.WithString("placeholder", mcpgo.Required(),
		mcpgo.Description("The placeholder uuid from the latest "+search+" result. Proves you searched for an existing "+kind+" first."))
}

func linkArgs() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{
		mcpgo.WithString("memory", mcpgo.Required(), mcpgo.Description("uuid of the memory")),
		mcpgo.WithString("type", mcpgo.Required(), linkTypeEnum, mcpgo.Description("How the memory relates to the entity")),
		mcpgo.WithString("entity", mcpgo.Required(), mcpgo.Description("uuid of the entity")),
	}
}

func edgeArgs() []mcpgo.ToolOption {
	return []mcpgo.ToolOption{
		mcpgo.WithString("from", mcpgo.Required(), mcpgo.Description("uuid of the entity the edge reads from")),
		mcpgo.WithString("type", mcpgo.Required(), edgeTypeEnum,
			mcpgo.Description("Relationship, read from `from` to `to`. Inverse names are accepted: from=Noah child_of to=David is stored as David parent_of Noah.")),
		mcpgo.WithString("to", mcpgo.Required(), mcpgo.Description("uuid of the entity the edge points to")),
	}
}

func tool(name, description string, opts ...mcpgo.ToolOption) mcpgo.Tool {
	return mcpgo.NewTool(name, append([]mcpgo.ToolOption{mcpgo.WithDescription(description)}, opts...)...)
}

func readOnly() mcpgo.ToolOption    { return mcpgo.WithReadOnlyHintAnnotation(true) }
func destructive() mcpgo.ToolOption { return mcpgo.WithDestructiveHintAnnotation(true) }
func idempotent() mcpgo.ToolOption  { return mcpgo.WithIdempotentHintAnnotation(true) }

// registerTools adds every Loci tool to s. Each is one service method.
func registerTools(s *server.MCPServer, svc *service.Service) {
	// Entities

	s.AddTool(tool("entity_search",
		"Find entities by name, alias or description; typos and partial names match. Returns ranked entities and a placeholder uuid, which entity_create requires if none of the results is the entity you mean. Each search replaces the previous placeholder.",
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Name, alias or words from the description")),
		mcpgo.WithString("type", entityTypeEnum, mcpgo.Description("Only return entities of this type")),
		limitArg,
		readOnly(),
	), handle(svc.EntitySearch))

	s.AddTool(tool("entity_create",
		"Create an entity. Search first: the placeholder must come from the latest entity_search. Names and aliases must be unique among entities of the same type, ignoring case.",
		placeholderArg("entity", "entity_search"),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Display name, single line, casing kept as given")),
		mcpgo.WithArray("aliases", mcpgo.WithStringItems(), mcpgo.Description(aliasesDescription)),
		mcpgo.WithString("description", mcpgo.Required(), mcpgo.Description("One line that tells this entity apart from others with similar names")),
		mcpgo.WithString("type", mcpgo.Required(), entityTypeEnum),
	), handle(svc.EntityCreate))

	s.AddTool(tool("entity_get",
		"Get an entity with its neighborhood: its edges to other entities (named as read from this entity, e.g. child_of) and the titles of its memories grouped by link type.",
		idArg("entity"),
		readOnly(),
	), handle(func(a idArgs) (service.EntityDetail, error) { return svc.EntityGet(a.ID) }))

	s.AddTool(tool("entity_update",
		"Edit an entity. Omitted fields are unchanged; aliases, when given, replaces the whole list. Renaming does not keep the old name: add it to aliases if it still matters.",
		idArg("entity"),
		mcpgo.WithString("name", mcpgo.Description("New display name")),
		mcpgo.WithArray("aliases", mcpgo.WithStringItems(), mcpgo.Description(aliasesDescription+" Replaces the existing list.")),
		mcpgo.WithString("description", mcpgo.Description("New one-line description")),
		mcpgo.WithString("type", entityTypeEnum, mcpgo.Description("New type; name uniqueness is checked against entities of this type")),
		idempotent(),
	), handle(svc.EntityUpdate))

	s.AddTool(tool("entity_delete",
		"Delete an entity with its edges and links. Refused, listing the memories, if any memory is linked to this entity alone: link those elsewhere or delete them first.",
		idArg("entity"),
		destructive(),
	), handle(func(a idArgs) (Deleted, error) { return Deleted{a.ID}, svc.EntityDelete(a.ID) }))

	// Edges

	s.AddTool(tool("edge_create",
		"Relate two entities. Creating an edge that already exists (in either order, for a symmetric type) returns the existing one. Read an entity's edges with entity_get.",
		append(edgeArgs(), idempotent())...,
	), handle(svc.EdgeCreate))

	s.AddTool(tool("edge_delete",
		"Remove an edge between two entities, given by the same triple entity_get shows or its inverse.",
		append(edgeArgs(), destructive())...,
	), handle(func(a service.EdgeInput) (Deleted, error) { return Deleted{a}, svc.EdgeDelete(a) }))

	// Memories

	s.AddTool(tool("memory_search",
		"Keyword search over memory titles and content. A fallback: when you know the entity, entity_get and memory_list find its memories more reliably. Returns ranked memories and a placeholder uuid, which memory_create requires. Each search replaces the previous placeholder.",
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Words to find in titles and content")),
		limitArg,
		readOnly(),
	), handle(svc.MemorySearch))

	s.AddTool(tool("memory_create",
		"Store a memory linked to one or more entities. Search first: the placeholder must come from the latest memory_search, which also shows whether the memory already exists and should be updated instead.",
		placeholderArg("memory", "memory_search"),
		mcpgo.WithString("title", mcpgo.Required(), mcpgo.Description("One-line summary")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("The memory itself, free text")),
		mcpgo.WithArray("links", mcpgo.Required(), mcpgo.MinItems(1),
			mcpgo.Description("The entities this memory is about, at least one. A memory can link to several entities, each with its own type."),
			mcpgo.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":   map[string]any{"type": "string", "enum": names(model.LinkTypes), "description": "How the memory relates to the entity"},
					"entity": map[string]any{"type": "string", "description": "uuid of the entity"},
				},
				"required": []string{"type", "entity"},
			}),
		),
	), handle(svc.MemoryCreate))

	s.AddTool(tool("memory_get",
		"Get a memory with its content and links.",
		idArg("memory"),
		readOnly(),
	), handle(func(a idArgs) (service.MemoryDetail, error) { return svc.MemoryGet(a.ID) }))

	s.AddTool(tool("memory_update",
		"Edit a memory's title or content. Omitted fields are unchanged. Change links with link_create and link_delete.",
		idArg("memory"),
		mcpgo.WithString("title", mcpgo.Description("New one-line summary")),
		mcpgo.WithString("content", mcpgo.Description("New content, replacing the old")),
		idempotent(),
	), handle(svc.MemoryUpdate))

	s.AddTool(tool("memory_delete",
		"Delete a memory and its links.",
		idArg("memory"),
		destructive(),
	), handle(func(a idArgs) (Deleted, error) { return Deleted{a.ID}, svc.MemoryDelete(a.ID) }))

	s.AddTool(tool("memory_list",
		"List the full memories linked to an entity, newest first, optionally only those linked by one type. A memory linked to the entity by two types appears once per type.",
		mcpgo.WithString("entity", mcpgo.Required(), mcpgo.Description("uuid of the entity")),
		mcpgo.WithString("link_type", linkTypeEnum, mcpgo.Description("Only memories linked by this type")),
		readOnly(),
	), handle(svc.MemoryList))

	// Links

	s.AddTool(tool("link_create",
		"Link an existing memory to another entity. Creating a link that already exists is a no-op.",
		append(linkArgs(), idempotent())...,
	), handle(svc.LinkCreate))

	s.AddTool(tool("link_delete",
		"Remove one link from a memory. Refused if it is the memory's last link: a memory must stay linked to at least one entity.",
		append(linkArgs(), destructive())...,
	), handle(func(a service.LinkInput) (Deleted, error) { return Deleted{a}, svc.LinkDelete(a) }))
}
