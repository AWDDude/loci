package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AWDDude/loci/internal/model"
)

func TestTrigramSimilarity(t *testing.T) {
	if got := similarity(trigrams("davd"), trigrams("David")); got < trigramThreshold {
		t.Errorf("typo similarity %.2f is below threshold", got)
	}
	if got := similarity(trigrams("david"), trigrams("David Kittle")); got < trigramThreshold {
		t.Errorf("first-name similarity %.2f is below threshold", got)
	}
	if got := similarity(trigrams("jq"), trigrams("Kubernetes")); got >= trigramThreshold {
		t.Errorf("unrelated similarity %.2f reaches threshold", got)
	}
	if got := similarity(trigrams("engRam"), trigrams("ENGRAM")); got != 1 {
		t.Errorf("case variants similarity %.2f, want 1", got)
	}
}

func TestBM25SetReplacesAndRemoveCleansUp(t *testing.T) {
	ix := newBM25Index()
	ix.set("a", field{"bbolt storage", 1})
	ix.set("a", field{"sqlite storage", 1})
	if _, ok := ix.score("bbolt")["a"]; ok {
		t.Error("re-indexed document still matches its old text")
	}
	if _, ok := ix.score("sqlite")["a"]; !ok {
		t.Error("re-indexed document does not match its new text")
	}
	ix.remove("a")
	if len(ix.postings) != 0 || ix.totalLen != 0 {
		t.Errorf("remove left postings %v, totalLen %d", ix.postings, ix.totalLen)
	}
}

func hitNames(hits []EntityHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Entity.Name
	}
	return out
}

func searchEntities(t *testing.T, s *Store, q string, typ model.EntityType) []string {
	t.Helper()
	hits, err := s.SearchEntities(q, typ, 0)
	if err != nil {
		t.Fatal(err)
	}
	return hitNames(hits)
}

func TestSearchEntities(t *testing.T) {
	s := openTest(t)
	mkEntity(t, s, model.EntityPerson, "David Kittle", "dk")
	mkEntity(t, s, model.EntityService, "david-ops")
	mkEntity(t, s, model.EntityTool, "jq")
	e, err := s.CreateEntity(placeholder(t, s, KindEntity), model.Entity{
		Name: "Noah", Description: "son of the user, likes dinosaurs", Type: model.EntityPerson,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		query string
		typ   model.EntityType
		first string
	}{
		{"exact name", "jq", "", "jq"},
		{"full name", "david kittle", "", "David Kittle"},
		{"typo in second word", "kitle", "", "David Kittle"},
		{"alias", "DK", "", "David Kittle"},
		{"description word", "dinosaurs", "", "Noah"},
		{"type filter", "david", model.EntityService, "david-ops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchEntities(t, s, tt.query, tt.typ)
			if len(got) == 0 || got[0] != tt.first {
				t.Fatalf("search %q = %v, want %q first", tt.query, got, tt.first)
			}
		})
	}

	// An ambiguous first name returns every candidate; which ranks first is up
	// to BM25 length normalization, and the caller picks.
	for _, q := range []string{"david", "davd"} {
		if got := searchEntities(t, s, q, ""); len(got) != 2 {
			t.Errorf("search %q = %v, want both davids", q, got)
		}
	}
	if got := searchEntities(t, s, "david", model.EntityTool); len(got) != 0 {
		t.Errorf("type filter leaked: %v", got)
	}
	if got := searchEntities(t, s, "zzzz", ""); len(got) != 0 {
		t.Errorf("nonsense query matched: %v", got)
	}
	if hits, _ := s.SearchEntities("david", "", 1); len(hits) != 1 {
		t.Errorf("limit 1 returned %d hits", len(hits))
	}

	e.Name = "Noah Kittle"
	e.Description = "son of the user"
	if _, err := s.UpdateEntity(e); err != nil {
		t.Fatal(err)
	}
	if got := searchEntities(t, s, "dinosaurs", ""); len(got) != 0 {
		t.Errorf("old description still indexed: %v", got)
	}
	if got := searchEntities(t, s, "kittle", ""); len(got) != 2 {
		t.Errorf("renamed entity not indexed: %v", got)
	}

	if err := s.DeleteEntity(e.ID); err != nil {
		t.Fatal(err)
	}
	if got := searchEntities(t, s, "noah", ""); len(got) != 0 {
		t.Errorf("deleted entity still indexed: %v", got)
	}
}

func TestSearchMemories(t *testing.T) {
	s := openTest(t)
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	p := placeholder(t, s, KindMemory)
	inContent, _, err := s.CreateMemory(p, model.Memory{Title: "storage choice", Content: "we picked bbolt over sqlite"}, []model.Link{link(model.LinkDecision, loci)})
	if err != nil {
		t.Fatal(err)
	}
	inTitle, _, err := s.CreateMemory(placeholder(t, s, KindMemory), model.Memory{Title: "bbolt locking", Content: "one process holds the file lock"}, []model.Link{link(model.LinkAttribute, loci)})
	if err != nil {
		t.Fatal(err)
	}

	hits, err := s.SearchMemories("bbolt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Memory.ID != inTitle.ID {
		t.Fatalf("hits = %+v, want the title match first", hits)
	}

	inContent.Content = "we picked sqlite"
	if _, err := s.UpdateMemory(inContent); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchMemories("bbolt", 0); len(hits) != 1 {
		t.Errorf("old content still indexed: %+v", hits)
	}
	if err := s.DeleteMemory(inTitle.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchMemories("bbolt", 0); len(hits) != 0 {
		t.Errorf("deleted memory still indexed: %+v", hits)
	}
}

func TestIndexesRebuiltOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loci.bbolt")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	loci := mkEntity(t, s, model.EntityProject, "Loci")
	mkMemory(t, s, "bbolt chosen", link(model.LinkDecision, loci))
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := searchEntities(t, s, "loci", ""); len(got) != 1 {
		t.Errorf("entity index not rebuilt: %v", got)
	}
	if hits, _ := s.SearchMemories("bbolt", 0); len(hits) != 1 {
		t.Errorf("memory index not rebuilt: %+v", hits)
	}
}

// TestConcurrentWritesAndSearches is for the race detector: searches must be
// able to run while writes update the indexes.
func TestConcurrentWritesAndSearches(t *testing.T) {
	s := openTest(t)
	// Creates are serialized among themselves: with one global placeholder,
	// concurrent creators just invalidate each other and retry, which tests
	// nothing here. The point is searches overlapping writes.
	var createMu sync.Mutex
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			createMu.Lock()
			defer createMu.Unlock()
			p, err := s.NewPlaceholder(KindEntity)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := s.CreateEntity(p, model.Entity{Name: fmt.Sprintf("tool %d", i), Description: "d", Type: model.EntityTool}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := s.SearchEntities("tool", "", 0); err != nil {
					t.Error(err)
				}
				if _, err := s.SearchMemories("anything", 0); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if got := searchEntities(t, s, "tool", model.EntityTool); len(got) != 8 {
		t.Errorf("found %d tools, want 8", len(got))
	}
}
