package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/service"
	"github.com/AWDDude/loci/internal/store"
)

// allTools is every tool Loci exposes. The CLI parity test in milestone 7
// checks the cobra commands against the server's registered tools; this list
// pins the server side.
var allTools = []string{
	"edge_create", "edge_delete",
	"entity_create", "entity_delete", "entity_get", "entity_search", "entity_update",
	"link_create", "link_delete",
	"memory_create", "memory_delete", "memory_get", "memory_list", "memory_search", "memory_update",
}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "loci.bbolt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	c, err := client.NewInProcessClient(NewServer(service.New(st), "test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	init := mcpgo.InitializeRequest{}
	init.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcpgo.Implementation{Name: "test", Version: "0"}
	if _, err := c.Initialize(ctx, init); err != nil {
		t.Fatal(err)
	}
	return c
}

// call invokes a tool and returns its result, failing the test on a
// transport error (a tool error is a normal result).
func call(t *testing.T, c *client.Client, name string, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// callOK invokes a tool, requires success, and decodes its structured
// content into out.
func callOK(t *testing.T, c *client.Client, name string, args map[string]any, out any) {
	t.Helper()
	res := call(t, c, name, args)
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: decoding %s: %v", name, data, err)
	}
}

func text(res *mcpgo.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestToolList(t *testing.T) {
	c := newClient(t)
	res, err := c.ListTools(context.Background(), mcpgo.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, allTools) {
		t.Fatalf("tools = %v\nwant    %v", got, allTools)
	}
}

// TestEnumsMatchModel guards invariant 5 at the schema level: the enums an
// agent sees are exactly the closed sets the service accepts.
func TestEnumsMatchModel(t *testing.T) {
	c := newClient(t)
	res, err := c.ListTools(context.Background(), mcpgo.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	enum := func(tool, prop string) []string {
		for _, tl := range res.Tools {
			if tl.Name != tool {
				continue
			}
			p, _ := tl.InputSchema.Properties[prop].(map[string]any)
			var out []string
			switch v := p["enum"].(type) {
			case []string:
				out = v
			case []any:
				for _, s := range v {
					out = append(out, s.(string))
				}
			}
			return out
		}
		t.Fatalf("no tool %s", tool)
		return nil
	}
	if got := enum("entity_create", "type"); !slices.Equal(got, names(model.EntityTypes)) {
		t.Errorf("entity type enum = %v", got)
	}
	if got := enum("link_create", "type"); !slices.Equal(got, names(model.LinkTypes)) {
		t.Errorf("link type enum = %v", got)
	}
	if got := enum("edge_create", "type"); !slices.Equal(got, model.EdgeTypeNames()) {
		t.Errorf("edge type enum = %v", got)
	}
}

func TestRoundTrip(t *testing.T) {
	c := newClient(t)

	var search service.EntitySearchResult
	callOK(t, c, "entity_search", map[string]any{"query": "loci"}, &search)
	if len(search.Entities) != 0 || search.Placeholder == "" {
		t.Fatalf("empty search = %+v", search)
	}

	var loci model.Entity
	callOK(t, c, "entity_create", map[string]any{
		"placeholder": search.Placeholder, "name": "Loci", "description": "entity memory server",
		"type": "project", "aliases": []string{"loci-mcp"},
	}, &loci)
	if loci.ID == "" || len(loci.Aliases) != 1 {
		t.Fatalf("created = %+v", loci)
	}

	var msearch service.MemorySearchResult
	callOK(t, c, "memory_search", map[string]any{"query": "bbolt"}, &msearch)
	var mem service.MemoryDetail
	callOK(t, c, "memory_create", map[string]any{
		"placeholder": msearch.Placeholder, "title": "uses bbolt", "content": "bbolt over sqlite",
		"links": []map[string]any{{"type": "decision", "entity": loci.ID}},
	}, &mem)
	if len(mem.Links) != 1 || mem.Links[0].Entity.Name != "Loci" {
		t.Fatalf("memory links = %+v", mem.Links)
	}

	var detail service.EntityDetail
	callOK(t, c, "entity_get", map[string]any{"id": loci.ID}, &detail)
	if len(detail.Memories[model.LinkDecision]) != 1 {
		t.Fatalf("entity_get memories = %+v", detail.Memories)
	}

	title := "chose bbolt"
	var updated model.Memory
	callOK(t, c, "memory_update", map[string]any{"id": mem.Memory.ID, "title": title}, &updated)
	if updated.Title != title || updated.Content != "bbolt over sqlite" {
		t.Fatalf("partial update over MCP = %+v", updated)
	}

	var deleted Deleted
	callOK(t, c, "memory_delete", map[string]any{"id": mem.Memory.ID}, &deleted)
	if deleted.Deleted != mem.Memory.ID {
		t.Fatalf("deleted = %+v", deleted)
	}
}

func TestErrorsAreToolResults(t *testing.T) {
	c := newClient(t)
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"stale placeholder", "entity_create", map[string]any{"placeholder": "old", "name": "x", "description": "d", "type": "tool"}, "search again"},
		{"unknown type", "entity_search", map[string]any{"query": "x", "type": "animal"}, "valid: person"},
		{"missing entity", "entity_get", map[string]any{"id": "nope"}, "not found"},
		{"wrong argument type", "entity_search", map[string]any{"query": "x", "limit": "ten"}, "invalid arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, c, tt.tool, tt.args)
			if !res.IsError || !strings.Contains(text(res), tt.want) {
				t.Fatalf("got error=%v %q, want a tool error containing %q", res.IsError, text(res), tt.want)
			}
		})
	}
}
