package store

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/AWDDude/loci/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data", "loci.bbolt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func placeholder(t *testing.T, s *Store, kind Kind) string {
	t.Helper()
	p, err := s.NewPlaceholder(kind)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mkEntity(t *testing.T, s *Store, typ model.EntityType, name string, aliases ...string) model.Entity {
	t.Helper()
	e, err := s.CreateEntity(placeholder(t, s, KindEntity), model.Entity{
		Name: name, Aliases: aliases, Description: "test " + name, Type: typ,
	})
	if err != nil {
		t.Fatalf("creating %s %q: %v", typ, name, err)
	}
	return e
}

func mkMemory(t *testing.T, s *Store, title string, links ...model.Link) model.Memory {
	t.Helper()
	m, _, err := s.CreateMemory(placeholder(t, s, KindMemory), model.Memory{Title: title, Content: "about " + title}, links)
	if err != nil {
		t.Fatalf("creating memory %q: %v", title, err)
	}
	return m
}

func link(typ model.LinkType, e model.Entity) model.Link {
	return model.Link{Type: typ, EntityID: e.ID}
}

func TestOpenReopensExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loci.bbolt")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEntity(placeholder(t, s, KindEntity), model.Entity{Name: "Loci", Description: "d", Type: model.EntityProject})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.GetEntity(e.ID); err != nil || got.Name != "Loci" {
		t.Fatalf("after reopen: %+v, %v", got, err)
	}
}

func TestPlaceholderRotation(t *testing.T) {
	s := openTest(t)
	e := model.Entity{Name: "David", Description: "d", Type: model.EntityPerson}

	if _, err := s.CreateEntity("never-issued", e); !errors.Is(err, ErrStalePlaceholder) {
		t.Fatalf("create before any search: want ErrStalePlaceholder, got %v", err)
	}

	first := placeholder(t, s, KindEntity)
	second := placeholder(t, s, KindEntity)
	if _, err := s.CreateEntity(first, e); !errors.Is(err, ErrStalePlaceholder) {
		t.Fatalf("superseded placeholder: want ErrStalePlaceholder, got %v", err)
	}
	if _, err := s.CreateEntity(second, e); err != nil {
		t.Fatalf("current placeholder: %v", err)
	}
	e.Name = "Emma"
	if _, err := s.CreateEntity(second, e); !errors.Is(err, ErrStalePlaceholder) {
		t.Fatalf("placeholder reused after a create: want ErrStalePlaceholder, got %v", err)
	}
}

func TestPlaceholderKindsAreIndependent(t *testing.T) {
	s := openTest(t)
	ent := placeholder(t, s, KindEntity)
	placeholder(t, s, KindMemory)
	if _, err := s.CreateEntity(ent, model.Entity{Name: "x", Description: "d", Type: model.EntityTool}); err != nil {
		t.Fatalf("a memory search must not invalidate the entity placeholder: %v", err)
	}
}

func TestFailedCreateKeepsPlaceholder(t *testing.T) {
	s := openTest(t)
	mkEntity(t, s, model.EntityPerson, "David")
	p := placeholder(t, s, KindEntity)
	dup := model.Entity{Name: "david", Description: "d", Type: model.EntityPerson}
	var taken *NameTakenError
	if _, err := s.CreateEntity(p, dup); !errors.As(err, &taken) {
		t.Fatalf("want NameTakenError, got %v", err)
	}
	dup.Name = "Dave"
	if _, err := s.CreateEntity(p, dup); err != nil {
		t.Fatalf("placeholder should survive a rejected create: %v", err)
	}
}

func TestNameUniquenessPerType(t *testing.T) {
	s := openTest(t)
	david := mkEntity(t, s, model.EntityPerson, "David Kittle", "dave")

	var taken *NameTakenError
	_, err := s.CreateEntity(placeholder(t, s, KindEntity), model.Entity{Name: "DAVE", Description: "d", Type: model.EntityPerson})
	if !errors.As(err, &taken) || taken.Existing.ID != david.ID {
		t.Fatalf("name matching another's alias: want NameTakenError for %s, got %v", david.ID, err)
	}
	_, err = s.CreateEntity(placeholder(t, s, KindEntity), model.Entity{Name: "Someone", Aliases: []string{"david kittle"}, Description: "d", Type: model.EntityPerson})
	if !errors.As(err, &taken) {
		t.Fatalf("alias matching another's name: want NameTakenError, got %v", err)
	}
	// Same name, different type, is fine.
	mkEntity(t, s, model.EntityService, "dave")
}

func TestUpdateEntity(t *testing.T) {
	s := openTest(t)
	mkEntity(t, s, model.EntityPlace, "Mercury")
	proj := mkEntity(t, s, model.EntityProject, "Mercury")

	// Renaming to its own name, differently cased, does not collide with itself.
	proj.Name = "MERCURY"
	got, err := s.UpdateEntity(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(proj.CreatedAt) || got.UpdatedAt.Before(proj.CreatedAt) {
		t.Errorf("timestamps: created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}

	// Changing type is checked against the new type.
	proj.Type = model.EntityPlace
	var taken *NameTakenError
	if _, err := s.UpdateEntity(proj); !errors.As(err, &taken) {
		t.Fatalf("type change into a taken name: want NameTakenError, got %v", err)
	}

	if _, err := s.UpdateEntity(model.Entity{ID: "missing", Name: "x", Type: model.EntityTool}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCreateMemoryRequiresLinks(t *testing.T) {
	s := openTest(t)
	p := placeholder(t, s, KindMemory)
	if _, _, err := s.CreateMemory(p, model.Memory{Title: "t", Content: "c"}, nil); !errors.Is(err, ErrNoLinks) {
		t.Fatalf("want ErrNoLinks, got %v", err)
	}
	_, _, err := s.CreateMemory(p, model.Memory{Title: "t", Content: "c"}, []model.Link{{Type: model.LinkMention, EntityID: "missing"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("link to missing entity: want ErrNotFound, got %v", err)
	}
}

func TestCreateMemoryCollapsesDuplicateLinks(t *testing.T) {
	s := openTest(t)
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	_, links, err := s.CreateMemory(placeholder(t, s, KindMemory), model.Memory{Title: "t", Content: "c"},
		[]model.Link{link(model.LinkDecision, loci), link(model.LinkDecision, loci), link(model.LinkEvent, loci)})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %+v, want 2", links)
	}
}

func TestLinks(t *testing.T) {
	s := openTest(t)
	david := mkEntity(t, s, model.EntityPerson, "David")
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	m := mkMemory(t, s, "uses bbolt", link(model.LinkDecision, loci))

	l := model.Link{MemoryID: m.ID, Type: model.LinkPreference, EntityID: david.ID}
	if _, err := s.CreateLink(l); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLink(l); err != nil {
		t.Fatalf("recreating an existing link should be a no-op: %v", err)
	}
	_, links, _ := s.GetMemory(m.ID)
	if len(links) != 2 {
		t.Fatalf("links = %+v, want 2", links)
	}

	if err := s.DeleteLink(l); err != nil {
		t.Fatal(err)
	}
	last := model.Link{MemoryID: m.ID, Type: model.LinkDecision, EntityID: loci.ID}
	if err := s.DeleteLink(last); !errors.Is(err, ErrLastLink) {
		t.Fatalf("want ErrLastLink, got %v", err)
	}
	if err := s.DeleteLink(l); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a missing link: want ErrNotFound, got %v", err)
	}
	if _, err := s.CreateLink(model.Link{MemoryID: "missing", Type: model.LinkMention, EntityID: loci.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("link from missing memory: want ErrNotFound, got %v", err)
	}
}

func TestEntityMemories(t *testing.T) {
	s := openTest(t)
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	a := mkMemory(t, s, "released", link(model.LinkEvent, loci))
	b := mkMemory(t, s, "bbolt", link(model.LinkDecision, loci), link(model.LinkEvent, loci))

	all, err := s.EntityMemories(loci.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d rows, want 3 (one per link)", len(all))
	}
	events, _ := s.EntityMemories(loci.ID, model.LinkEvent)
	var ids []string
	for _, lm := range events {
		ids = append(ids, lm.Memory.ID)
	}
	slices.Sort(ids)
	want := []string{a.ID, b.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("event memories = %v, want %v", ids, want)
	}
	if _, err := s.EntityMemories("missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpdateAndDeleteMemory(t *testing.T) {
	s := openTest(t)
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	m := mkMemory(t, s, "old", link(model.LinkMention, loci))

	m.Title, m.Content = "new", "new content"
	if _, err := s.UpdateMemory(m); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.GetMemory(m.ID)
	if got.Title != "new" || got.Content != "new content" {
		t.Fatalf("after update: %+v", got)
	}

	if err := s.DeleteMemory(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetMemory(m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
	if rows, _ := s.EntityMemories(loci.ID, ""); len(rows) != 0 {
		t.Fatalf("links survived memory delete: %+v", rows)
	}
}

func TestEdges(t *testing.T) {
	s := openTest(t)
	david := mkEntity(t, s, model.EntityPerson, "David")
	noah := mkEntity(t, s, model.EntityPerson, "Noah")
	emma := mkEntity(t, s, model.EntityPerson, "Emma")

	if _, err := s.CreateEdge(model.Edge{From: david.ID, Type: model.EdgeParentOf, To: noah.ID}); err != nil {
		t.Fatal(err)
	}
	spouse, err := s.CreateEdge(model.Edge{From: emma.ID, Type: model.EdgeSpouseOf, To: david.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The reverse of a symmetric edge is the same edge.
	again, err := s.CreateEdge(model.Edge{From: david.ID, Type: model.EdgeSpouseOf, To: emma.ID})
	if err != nil || again != spouse {
		t.Fatalf("reverse symmetric edge = %+v, %v; want existing %+v", again, err, spouse)
	}

	edges, err := s.EntityEdges(david.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("david's edges = %+v, want 2", edges)
	}
	noahEdges, _ := s.EntityEdges(noah.ID)
	if len(noahEdges) != 1 || noahEdges[0].Name != "child_of" || noahEdges[0].Other != david.ID {
		t.Fatalf("noah's edges = %+v, want child_of david", noahEdges)
	}

	if _, err := s.CreateEdge(model.Edge{From: david.ID, Type: model.EdgeUses, To: david.ID}); !errors.Is(err, ErrSelfEdge) {
		t.Fatalf("want ErrSelfEdge, got %v", err)
	}
	if _, err := s.CreateEdge(model.Edge{From: david.ID, Type: model.EdgeUses, To: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if err := s.DeleteEdge(model.Edge{From: david.ID, Type: model.EdgeSpouseOf, To: emma.ID}); err != nil {
		t.Fatalf("deleting a symmetric edge by its reverse: %v", err)
	}
	if err := s.DeleteEdge(model.Edge{From: david.ID, Type: model.EdgeSpouseOf, To: emma.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if edges, _ := s.EntityEdges(emma.ID); len(edges) != 0 {
		t.Fatalf("emma's edges after delete = %+v", edges)
	}
}

func TestDeleteEntity(t *testing.T) {
	s := openTest(t)
	david := mkEntity(t, s, model.EntityPerson, "David")
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	only := mkMemory(t, s, "only loci", link(model.LinkDecision, loci), link(model.LinkEvent, loci))
	shared := mkMemory(t, s, "shared", link(model.LinkDecision, loci), link(model.LinkPreference, david))
	if _, err := s.CreateEdge(model.Edge{From: david.ID, Type: model.EdgeWorksOn, To: loci.ID}); err != nil {
		t.Fatal(err)
	}

	var inUse *EntityInUseError
	if err := s.DeleteEntity(loci.ID); !errors.As(err, &inUse) {
		t.Fatalf("want EntityInUseError, got %v", err)
	}
	if len(inUse.Memories) != 1 || inUse.Memories[0].ID != only.ID {
		t.Fatalf("blocking memories = %+v, want only %s", inUse.Memories, only.ID)
	}

	if err := s.DeleteMemory(only.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEntity(loci.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEntity(loci.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_, links, _ := s.GetMemory(shared.ID)
	if len(links) != 1 || links[0].EntityID != david.ID {
		t.Fatalf("shared memory links = %+v, want only david", links)
	}
	if edges, _ := s.EntityEdges(david.ID); len(edges) != 0 {
		t.Fatalf("edges survived entity delete: %+v", edges)
	}
	if err := s.DeleteEntity(loci.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSecondOpenTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loci.bbolt")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("a second open of a locked database should fail, not hang")
	}
}
