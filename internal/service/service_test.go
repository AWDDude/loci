package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/store"
)

func newTest(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "loci.bbolt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

// createEntity searches first, as every caller must, then creates.
func createEntity(t *testing.T, s *Service, typ, name, description string, aliases ...string) model.Entity {
	t.Helper()
	res, err := s.EntitySearch(EntitySearchInput{Query: name})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.EntityCreate(EntityCreateInput{
		Placeholder: res.Placeholder, Name: name, Aliases: aliases, Description: description, Type: typ,
	})
	if err != nil {
		t.Fatalf("creating %s %q: %v", typ, name, err)
	}
	return e
}

func createMemory(t *testing.T, s *Service, title, content string, links ...LinkSpec) MemoryDetail {
	t.Helper()
	res, err := s.MemorySearch(MemorySearchInput{Query: title})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.MemoryCreate(MemoryCreateInput{Placeholder: res.Placeholder, Title: title, Content: content, Links: links})
	if err != nil {
		t.Fatalf("creating memory %q: %v", title, err)
	}
	return d
}

// TestReadmeWalk is the navigation the README uses to explain Loci: find an
// entity, follow an edge, read the memories linked by one type.
func TestReadmeWalk(t *testing.T) {
	s := newTest(t)
	david := createEntity(t, s, "person", "David", "the user")
	noah := createEntity(t, s, "person", "Noah", "the user's son")
	if _, err := s.EdgeCreate(EdgeInput{From: david.ID, Type: "parent_of", To: noah.ID}); err != nil {
		t.Fatal(err)
	}
	createMemory(t, s, "Noah's birthday", "Noah's birthday is March 3", LinkSpec{Type: "attribute", Entity: noah.ID})

	found, err := s.EntitySearch(EntitySearchInput{Query: "david"})
	if err != nil || len(found.Entities) == 0 || found.Entities[0].Entity.ID != david.ID {
		t.Fatalf("step 1, search david: %+v, %v", found, err)
	}

	detail, err := s.EntityGet(david.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Edges) != 1 || detail.Edges[0].Name != "parent_of" || detail.Edges[0].Other.ID != noah.ID {
		t.Fatalf("step 2, david's edges: %+v", detail.Edges)
	}

	res, err := s.MemoryList(MemoryListInput{Entity: noah.ID, LinkType: "attribute"})
	mems := res.Memories
	if err != nil || len(mems) != 1 || !strings.Contains(mems[0].Memory.Content, "March 3") {
		t.Fatalf("step 3, noah's attributes: %+v, %v", mems, err)
	}
}

func TestSearchRotatesPlaceholder(t *testing.T) {
	s := newTest(t)
	first, err := s.EntitySearch(EntitySearchInput{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.EntitySearch(EntitySearchInput{Query: "x"})
	if first.Placeholder == "" || first.Placeholder == second.Placeholder {
		t.Fatalf("placeholders %q, %q: want distinct and non-empty", first.Placeholder, second.Placeholder)
	}
	_, err = s.EntityCreate(EntityCreateInput{Placeholder: first.Placeholder, Name: "x", Description: "d", Type: "tool"})
	if !errors.Is(err, store.ErrStalePlaceholder) || !strings.Contains(err.Error(), "search again") {
		t.Fatalf("stale placeholder: got %v", err)
	}
	if first.Entities == nil {
		t.Error("empty results should serialize as [], not null")
	}
}

func TestEntityValidation(t *testing.T) {
	s := newTest(t)
	res, _ := s.EntitySearch(EntitySearchInput{Query: "x"})
	tests := []struct {
		name string
		in   EntityCreateInput
		want string
	}{
		{"bad type", EntityCreateInput{Name: "x", Description: "d", Type: "animal"}, "unknown entity type"},
		{"empty name", EntityCreateInput{Name: "  ", Description: "d", Type: "tool"}, "name must not be empty"},
		{"multiline name", EntityCreateInput{Name: "a\nb", Description: "d", Type: "tool"}, "single line"},
		{"no description", EntityCreateInput{Name: "x", Type: "tool"}, "description must not be empty"},
		{"no placeholder", EntityCreateInput{Placeholder: " ", Name: "x", Description: "d", Type: "tool"}, "placeholder is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.Placeholder == "" {
				tt.in.Placeholder = res.Placeholder
			}
			_, err := s.EntityCreate(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
	if _, err := s.EntitySearch(EntitySearchInput{Query: "x", Type: "animal"}); err == nil {
		t.Error("search with an unknown type filter should fail")
	}
	if _, err := s.EntitySearch(EntitySearchInput{Query: " "}); err == nil {
		t.Error("search with an empty query should fail")
	}
}

func TestEntityCreateNormalizes(t *testing.T) {
	s := newTest(t)
	e := createEntity(t, s, "tool", "  jq ", "  JSON   processor ", "JQ", "jquery-json")
	if e.Name != "jq" || e.Description != "JSON processor" {
		t.Errorf("not normalized: %+v", e)
	}
	if len(e.Aliases) != 1 || e.Aliases[0] != "jquery-json" {
		t.Errorf("alias equal to the name should be dropped: %q", e.Aliases)
	}
}

func TestEntityUpdatePartial(t *testing.T) {
	s := newTest(t)
	e := createEntity(t, s, "person", "Davd", "the user", "dk")

	name := "David"
	got, err := s.EntityUpdate(EntityUpdateInput{ID: e.ID, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "David" || got.Description != "the user" || len(got.Aliases) != 1 {
		t.Fatalf("partial update changed other fields: %+v", got)
	}
	for _, a := range got.Aliases {
		if a == "Davd" {
			t.Error("rename must not add the old name as an alias")
		}
	}

	none := []string{}
	if got, _ = s.EntityUpdate(EntityUpdateInput{ID: e.ID, Aliases: &none}); len(got.Aliases) != 0 {
		t.Errorf("aliases not replaced: %q", got.Aliases)
	}
	bad := "animal"
	if _, err := s.EntityUpdate(EntityUpdateInput{ID: e.ID, Type: &bad}); err == nil {
		t.Error("update to an unknown type should fail")
	}
}

func TestMemoryCreateValidation(t *testing.T) {
	s := newTest(t)
	loci := createEntity(t, s, "project", "Loci", "entity memory")
	res, _ := s.MemorySearch(MemorySearchInput{Query: "x"})
	ok := []LinkSpec{{Type: "decision", Entity: loci.ID}}

	tests := []struct {
		name string
		in   MemoryCreateInput
		want string
	}{
		{"no links", MemoryCreateInput{Title: "t", Content: "c"}, "at least one entity"},
		{"bad link type", MemoryCreateInput{Title: "t", Content: "c", Links: []LinkSpec{{Type: "tag", Entity: loci.ID}}}, "unknown link type"},
		{"missing entity", MemoryCreateInput{Title: "t", Content: "c", Links: []LinkSpec{{Type: "mention", Entity: "nope"}}}, "not found"},
		{"multiline title", MemoryCreateInput{Title: "a\nb", Content: "c", Links: ok}, "single line"},
		{"blank content", MemoryCreateInput{Title: "t", Content: "\n", Links: ok}, "content must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.Placeholder = res.Placeholder
			_, err := s.MemoryCreate(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestEntityGetGroupsMemoriesByLinkType(t *testing.T) {
	s := newTest(t)
	loci := createEntity(t, s, "project", "Loci", "entity memory")
	david := createEntity(t, s, "person", "David", "the user")
	createMemory(t, s, "uses bbolt", "bbolt over sqlite", LinkSpec{Type: "decision", Entity: loci.ID})
	createMemory(t, s, "v0 released", "first release", LinkSpec{Type: "event", Entity: loci.ID}, LinkSpec{Type: "mention", Entity: david.ID})

	d, err := s.EntityGet(loci.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Memories[model.LinkDecision]) != 1 || len(d.Memories[model.LinkEvent]) != 1 || len(d.Memories) != 2 {
		t.Fatalf("memories = %+v", d.Memories)
	}
	if d.Edges == nil {
		t.Error("no edges should serialize as [], not null")
	}

	m, err := s.MemoryGet(d.Memories[model.LinkEvent][0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Links) != 2 {
		t.Fatalf("links = %+v, want 2", m.Links)
	}
	for _, l := range m.Links {
		if l.Entity.Name == "" {
			t.Errorf("link entity not resolved: %+v", l)
		}
	}
}

func TestMemoryUpdatePartial(t *testing.T) {
	s := newTest(t)
	loci := createEntity(t, s, "project", "Loci", "entity memory")
	d := createMemory(t, s, "old title", "content", LinkSpec{Type: "mention", Entity: loci.ID})

	title := "new title"
	got, err := s.MemoryUpdate(MemoryUpdateInput{ID: d.Memory.ID, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new title" || got.Content != "content" {
		t.Fatalf("partial update: %+v", got)
	}
}

func TestEdgeInverseNames(t *testing.T) {
	s := newTest(t)
	david := createEntity(t, s, "person", "David", "the user")
	noah := createEntity(t, s, "person", "Noah", "the user's son")

	e, err := s.EdgeCreate(EdgeInput{From: noah.ID, Type: "child_of", To: david.ID})
	if err != nil {
		t.Fatal(err)
	}
	if e.From != david.ID || e.Type != model.EdgeParentOf || e.To != noah.ID {
		t.Fatalf("inverse name not stored in forward form: %+v", e)
	}
	d, _ := s.EntityGet(noah.ID)
	if len(d.Edges) != 1 || d.Edges[0].Name != "child_of" {
		t.Fatalf("noah's edges = %+v", d.Edges)
	}
	if err := s.EdgeDelete(EdgeInput{From: david.ID, Type: "parent_of", To: noah.ID}); err != nil {
		t.Fatalf("deleting by the forward name: %v", err)
	}
	if _, err := s.EdgeCreate(EdgeInput{From: david.ID, Type: "likes", To: noah.ID}); err == nil {
		t.Error("unknown edge type should fail")
	}
}

func TestLinkCreateAndDelete(t *testing.T) {
	s := newTest(t)
	loci := createEntity(t, s, "project", "Loci", "entity memory")
	david := createEntity(t, s, "person", "David", "the user")
	d := createMemory(t, s, "prefers jq", "jq for JSON", LinkSpec{Type: "mention", Entity: loci.ID})

	in := LinkInput{Memory: d.Memory.ID, Type: "preference", Entity: david.ID}
	if _, err := s.LinkCreate(in); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkDelete(LinkInput{Memory: d.Memory.ID, Type: "mention", Entity: loci.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkDelete(in); !errors.Is(err, store.ErrLastLink) {
		t.Fatalf("want ErrLastLink, got %v", err)
	}
	if _, err := s.LinkCreate(LinkInput{Type: "mention", Entity: loci.ID}); err == nil {
		t.Error("link without a memory should fail")
	}
}

func TestEntityDeleteRefusalNamesMemories(t *testing.T) {
	s := newTest(t)
	loci := createEntity(t, s, "project", "Loci", "entity memory")
	createMemory(t, s, "uses bbolt", "bbolt", LinkSpec{Type: "decision", Entity: loci.ID})

	err := s.EntityDelete(loci.ID)
	var inUse *store.EntityInUseError
	if !errors.As(err, &inUse) || !strings.Contains(err.Error(), "uses bbolt") {
		t.Fatalf("got %v, want refusal naming the memory", err)
	}
}
