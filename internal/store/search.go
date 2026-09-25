package store

import (
	"encoding/json"
	"fmt"
	"sort"

	bolt "go.etcd.io/bbolt"

	"github.com/AWDDude/loci/internal/model"
)

const (
	// nameWeight counts each name and alias term this many times in the
	// entity BM25 index, so a word in a name outranks the same word in some
	// other entity's description.
	nameWeight = 2
	// titleWeight does the same for memory titles over content.
	titleWeight = 2

	// rrfK is the standard Reciprocal Rank Fusion constant. It damps the top
	// ranks so neither the trigram nor the BM25 ranking dominates the fusion.
	rrfK = 60.0
)

// entityIndex holds what entity search needs in memory: a BM25 index over
// names, aliases and description, and the trigram sets of each name and alias
// (see nameTrigrams) for fuzzy matching.
type entityIndex struct {
	bm25  *bm25Index
	names map[string][]map[string]struct{}
	types map[string]model.EntityType
}

func newEntityIndex() *entityIndex {
	return &entityIndex{
		bm25:  newBM25Index(),
		names: map[string][]map[string]struct{}{},
		types: map[string]model.EntityType{},
	}
}

func (ix *entityIndex) set(e model.Entity) {
	fields := []field{{e.Description, 1}}
	sets := make([]map[string]struct{}, 0, 1+len(e.Aliases))
	for _, n := range e.Names() {
		fields = append(fields, field{n, nameWeight})
		sets = append(sets, nameTrigrams(n)...)
	}
	ix.bm25.set(e.ID, fields...)
	ix.names[e.ID] = sets
	ix.types[e.ID] = e.Type
}

func (ix *entityIndex) remove(id string) {
	ix.bm25.remove(id)
	delete(ix.names, id)
	delete(ix.types, id)
}

func setMemoryIndex(ix *bm25Index, m model.Memory) {
	ix.set(m.ID, field{m.Title, titleWeight}, field{m.Content, 1})
}

// rebuildIndexes fills the in-memory indexes from the records. Called once by
// Open, before the store is shared.
func (s *Store) rebuildIndexes() error {
	s.entityIx = newEntityIndex()
	s.memoryIx = newBM25Index()
	return s.db.View(func(tx *bolt.Tx) error {
		err := tx.Bucket(bucketEntities).ForEach(func(k, v []byte) error {
			var e model.Entity
			if err := json.Unmarshal(v, &e); err != nil {
				return fmt.Errorf("decoding entity %s: %w", k, err)
			}
			s.entityIx.set(e)
			return nil
		})
		if err != nil {
			return err
		}
		return tx.Bucket(bucketMemories).ForEach(func(k, v []byte) error {
			var m model.Memory
			if err := json.Unmarshal(v, &m); err != nil {
				return fmt.Errorf("decoding memory %s: %w", k, err)
			}
			setMemoryIndex(s.memoryIx, m)
			return nil
		})
	})
}

// EntityHit is one entity search result.
type EntityHit struct {
	Entity model.Entity `json:"entity"`
	Score  float64      `json:"score"`
}

// SearchEntities ranks entities against query by fusing two rankings: the best
// trigram similarity of any of an entity's names (catching typos and partial
// names), and BM25 over names, aliases and description (catching words). An
// empty typ searches every type. limit <= 0 returns every match.
func (s *Store) SearchEntities(query string, typ model.EntityType, limit int) ([]EntityHit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := trigrams(query)
	fuzzy := map[string]float64{}
	for id, sets := range s.entityIx.names {
		if typ != "" && s.entityIx.types[id] != typ {
			continue
		}
		best := 0.0
		for _, set := range sets {
			best = max(best, similarity(q, set))
		}
		if best >= trigramThreshold {
			fuzzy[id] = best
		}
	}
	words := s.entityIx.bm25.score(query)
	if typ != "" {
		for id := range words {
			if s.entityIx.types[id] != typ {
				delete(words, id)
			}
		}
	}

	ids, scores := topN(fuse(rank(fuzzy), rank(words)), limit)
	hits := make([]EntityHit, len(ids))
	err := s.db.View(func(tx *bolt.Tx) error {
		for i, id := range ids {
			hits[i].Score = scores[i]
			if err := getJSON(tx.Bucket(bucketEntities), "entity", id, &hits[i].Entity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// MemoryHit is one memory search result.
type MemoryHit struct {
	Memory model.Memory `json:"memory"`
	Score  float64      `json:"score"`
}

// SearchMemories ranks memories against query by BM25 over title and
// content. limit <= 0 returns every match.
func (s *Store) SearchMemories(query string, limit int) ([]MemoryHit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, scores := topN(s.memoryIx.score(query), limit)
	hits := make([]MemoryHit, len(ids))
	err := s.db.View(func(tx *bolt.Tx) error {
		for i, id := range ids {
			hits[i].Score = scores[i]
			if err := getJSON(tx.Bucket(bucketMemories), "memory", id, &hits[i].Memory); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// rank orders ids by score, best first, breaking ties by id so results are
// deterministic.
func rank(scores map[string]float64) []string {
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

// fuse combines rankings with Reciprocal Rank Fusion: each ranking adds
// 1/(rrfK + rank) for every id it contains. Ranks rather than raw scores are
// fused because trigram similarity and BM25 are on unrelated scales.
func fuse(rankings ...[]string) map[string]float64 {
	out := map[string]float64{}
	for _, r := range rankings {
		for i, id := range r {
			out[id] += 1 / (rrfK + float64(i+1))
		}
	}
	return out
}

// topN returns the best limit ids and their scores, or all of them when
// limit <= 0.
func topN(scores map[string]float64, limit int) ([]string, []float64) {
	ids := rank(scores)
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]float64, len(ids))
	for i, id := range ids {
		out[i] = scores[id]
	}
	return ids, out
}
