package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/mcp"
	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/service"
	"github.com/AWDDude/loci/internal/store"
)

// harness runs CLI commands against an in-process MCP server, so these tests
// cover argument mapping and output formatting without a daemon. The daemon
// path is covered by cmd/loci's integration test.
type harness struct {
	t      *testing.T
	env    *Env
	config string
	json   bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "loci.bbolt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := mcp.NewServer(service.New(st), "test")

	h := &harness{t: t}
	h.env = &Env{
		ConfigPath: &h.config,
		JSON:       &h.json,
		Version:    "test",
		Connect: func(ctx context.Context) (Caller, error) {
			c, err := client.NewInProcessClient(srv)
			if err != nil {
				return nil, err
			}
			return NewMCPCaller(ctx, c)
		},
	}
	return h
}

// run executes `loci <args>` with stdin and returns stdout.
func (h *harness) run(stdin string, args ...string) (string, error) {
	h.t.Helper()
	root := &cobra.Command{Use: "loci", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().BoolVar(&h.json, "json", false, "")
	AddCommands(root, h.env)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func (h *harness) ok(args ...string) string {
	h.t.Helper()
	out, err := h.run("", args...)
	if err != nil {
		h.t.Fatalf("loci %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// okJSON runs a command with --json and decodes its output into v.
func (h *harness) okJSON(v any, args ...string) {
	h.t.Helper()
	out := h.ok(append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		h.t.Fatalf("decoding %q: %v", out, err)
	}
}

func (h *harness) entityPlaceholder(query string) string {
	var r service.EntitySearchResult
	h.okJSON(&r, "entity", "search", query)
	return r.Placeholder
}

func (h *harness) memoryPlaceholder(query string) string {
	var r service.MemorySearchResult
	h.okJSON(&r, "memory", "search", query)
	return r.Placeholder
}

func (h *harness) createEntity(typ, name, description string) model.Entity {
	var e model.Entity
	h.okJSON(&e, "entity", "create", h.entityPlaceholder(name), "--type", typ, "--name", name, "--description", description)
	return e
}

func TestEntitySearchShowsPlaceholderRow(t *testing.T) {
	h := newHarness(t)
	h.createEntity("person", "David Kittle", "the user")

	out := h.ok("entity", "search", "david")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header, one hit and the (new) row:\n%s", out)
	}
	if !strings.Contains(lines[1], "David Kittle") || !strings.Contains(lines[2], "(new)") {
		t.Errorf("unexpected table:\n%s", out)
	}
}

func TestEntityLifecycle(t *testing.T) {
	h := newHarness(t)
	p := h.entityPlaceholder("davd")
	out := h.ok("entity", "create", p, "--type", "person", "--name", "Davd", "--description", "the user", "--aliases", "dk,dave")
	if !strings.Contains(out, "Davd (person)") || !strings.Contains(out, "aliases: dk, dave") {
		t.Fatalf("create output:\n%s", out)
	}

	var res service.EntitySearchResult
	h.okJSON(&res, "entity", "search", "davd")
	e := res.Entities[0].Entity

	// Only the flags given change.
	h.okJSON(&e, "entity", "update", e.ID, "--name", "David")
	if e.Name != "David" || e.Description != "the user" || len(e.Aliases) != 2 {
		t.Fatalf("partial update: %+v", e)
	}
	h.okJSON(&e, "entity", "update", e.ID, "--aliases", "")
	if len(e.Aliases) != 0 {
		t.Fatalf(`--aliases "" should clear aliases: %q`, e.Aliases)
	}

	if out := h.ok("entity", "delete", e.ID); !strings.Contains(out, "Deleted entity "+e.ID) {
		t.Fatalf("delete output: %q", out)
	}
}

func TestMemoryFlow(t *testing.T) {
	h := newHarness(t)
	david := h.createEntity("person", "David", "the user")
	noah := h.createEntity("person", "Noah", "the user's son")

	// An inverse edge name, as entity get would show it from Noah's side.
	var edge model.Edge
	h.okJSON(&edge, "edge", "create", noah.ID, "child_of", david.ID)
	if edge.From != david.ID || edge.Type != model.EdgeParentOf {
		t.Fatalf("edge = %+v", edge)
	}

	p := h.memoryPlaceholder("birthday")
	out, err := h.run("Noah's birthday\nis March 3", "memory", "create", p,
		"--title", "Noah's birthday", "--content", "-", "--link", "attribute:"+noah.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "attribute  Noah (person)") || !strings.Contains(out, "is March 3") {
		t.Fatalf("memory create output:\n%s", out)
	}

	out = h.ok("entity", "get", david.ID)
	if !strings.Contains(out, "parent_of  Noah (person)") {
		t.Fatalf("entity get output:\n%s", out)
	}
	out = h.ok("entity", "get", noah.ID)
	if !strings.Contains(out, "child_of  David (person)") || !strings.Contains(out, "attribute  Noah's birthday") {
		t.Fatalf("entity get output:\n%s", out)
	}

	out = h.ok("memory", "list", noah.ID, "--link-type", "attribute")
	if !strings.Contains(out, "[attribute] Noah's birthday") || !strings.Contains(out, "  is March 3") {
		t.Fatalf("memory list output:\n%s", out)
	}
	var list service.MemoryListResult
	h.okJSON(&list, "memory", "list", noah.ID)
	m := list.Memories[0].Memory

	var link model.Link
	h.okJSON(&link, "link", "create", m.ID, "mention", david.ID)
	if out := h.ok("link", "delete", m.ID, "mention", david.ID); !strings.Contains(out, "Deleted link") {
		t.Fatalf("link delete output: %q", out)
	}

	var updated model.Memory
	h.okJSON(&updated, "memory", "update", m.ID, "--title", "Noah's birthday (March 3)")
	if updated.Content != "Noah's birthday\nis March 3" {
		t.Fatalf("title-only update changed content: %q", updated.Content)
	}
	if out := h.ok("edge", "delete", david.ID, "parent_of", noah.ID); !strings.Contains(out, "Deleted edge") {
		t.Fatalf("edge delete output: %q", out)
	}
}

func TestErrorsReachTheUser(t *testing.T) {
	h := newHarness(t)
	loci := h.createEntity("project", "Loci", "entity memory")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"stale placeholder", []string{"entity", "create", "old", "--type", "tool", "--name", "x", "--description", "d"}, "search again"},
		{"missing required flag", []string{"entity", "create", "p", "--name", "x"}, "required flag"},
		{"bad link flag", []string{"memory", "create", "p", "--title", "t", "--content", "c", "--link", loci.ID}, "TYPE:ENTITY_UUID"},
		{"last link", nil, "last link"},
	}
	p := h.memoryPlaceholder("x")
	var d service.MemoryDetail
	h.okJSON(&d, "memory", "create", p, "--title", "t", "--content", "c", "--link", "decision:"+loci.ID)
	tests[3].args = []string{"link", "delete", d.Memory.ID, "decision", loci.ID}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.run("", tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestJSONIsTheToolResult checks that --json prints the tool's structured
// result unchanged, which is the contract that makes the CLI scriptable.
func TestJSONIsTheToolResult(t *testing.T) {
	h := newHarness(t)
	out := h.ok("entity", "search", "anything", "--json")
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	if _, ok := raw["placeholder"]; !ok {
		t.Errorf("missing placeholder: %s", out)
	}
	if ents, ok := raw["entities"].([]any); !ok || len(ents) != 0 {
		t.Errorf("entities = %v, want []", raw["entities"])
	}
}
