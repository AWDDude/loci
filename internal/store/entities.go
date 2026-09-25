package store

import (
	"encoding/json"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"

	"github.com/AWDDude/loci/internal/model"
)

// NameTakenError rejects an entity name or alias that another entity of the
// same type already answers to.
type NameTakenError struct {
	Name     string
	Type     model.EntityType
	Existing model.Entity
}

func (e *NameTakenError) Error() string {
	return fmt.Sprintf("%s %q is already taken by %s (%s)", e.Type, e.Name, e.Existing.Name, e.Existing.ID)
}

// MemoryRef identifies a memory in an error without carrying its content.
type MemoryRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// EntityInUseError refuses to delete an entity that is the only one some
// memories link to, since deleting it would leave them with no links.
type EntityInUseError struct {
	EntityID string
	Memories []MemoryRef
}

func (e *EntityInUseError) Error() string {
	titles := make([]string, len(e.Memories))
	for i, m := range e.Memories {
		titles[i] = fmt.Sprintf("%q (%s)", m.Title, m.ID)
	}
	return fmt.Sprintf("entity %s is the only link of %d memories; link them elsewhere or delete them first: %s",
		e.EntityID, len(e.Memories), strings.Join(titles, ", "))
}

// CreateEntity stores e under a new uuid. placeholder must be the current
// entity placeholder.
func (s *Store) CreateEntity(placeholder string, e model.Entity) (model.Entity, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := consumePlaceholder(tx, KindEntity, placeholder); err != nil {
			return err
		}
		e.ID = newID()
		if err := checkUnique(tx, e); err != nil {
			return err
		}
		e.CreatedAt = s.now()
		e.UpdatedAt = e.CreatedAt
		return putJSON(tx.Bucket(bucketEntities), e.ID, e)
	})
	if err != nil {
		return model.Entity{}, err
	}
	return e, nil
}

// GetEntity returns the entity with the given uuid.
func (s *Store) GetEntity(id string) (model.Entity, error) {
	var e model.Entity
	err := s.db.View(func(tx *bolt.Tx) error {
		return getJSON(tx.Bucket(bucketEntities), "entity", id, &e)
	})
	return e, err
}

// UpdateEntity replaces the name, aliases, description and type of the
// entity with e.ID. Uniqueness is checked against e.Type, so changing type
// is checked against the entities of the new type.
func (s *Store) UpdateEntity(e model.Entity) (model.Entity, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEntities)
		var old model.Entity
		if err := getJSON(b, "entity", e.ID, &old); err != nil {
			return err
		}
		if err := checkUnique(tx, e); err != nil {
			return err
		}
		e.CreatedAt = old.CreatedAt
		e.UpdatedAt = s.now()
		return putJSON(b, e.ID, e)
	})
	if err != nil {
		return model.Entity{}, err
	}
	return e, nil
}

// DeleteEntity removes the entity with its edges and links. It is refused
// with an *EntityInUseError if any memory links to no other entity.
func (s *Store) DeleteEntity(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		entities := tx.Bucket(bucketEntities)
		if entities.Get([]byte(id)) == nil {
			return notFound("entity", id)
		}

		links, err := entityLinks(tx, id)
		if err != nil {
			return err
		}
		var orphaned []MemoryRef
		checked := map[string]bool{}
		for _, l := range links {
			if checked[l.MemoryID] {
				continue
			}
			checked[l.MemoryID] = true
			others, err := linksToOtherEntities(tx, l.MemoryID, id)
			if err != nil {
				return err
			}
			if !others {
				var m model.Memory
				if err := getJSON(tx.Bucket(bucketMemories), "memory", l.MemoryID, &m); err != nil {
					return err
				}
				orphaned = append(orphaned, MemoryRef{ID: m.ID, Title: m.Title})
			}
		}
		if len(orphaned) > 0 {
			return &EntityInUseError{EntityID: id, Memories: orphaned}
		}

		for _, l := range links {
			if err := deleteLink(tx, l); err != nil {
				return err
			}
		}
		edges, err := entityEdges(tx, id)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if err := deleteEdge(tx, e.Edge); err != nil {
				return err
			}
		}
		return entities.Delete([]byte(id))
	})
}

// checkUnique rejects e if any other entity of its type answers to one of
// its names. The scan compares folds on the fly rather than keeping a stored
// index of folded names, which could drift from the names themselves.
func checkUnique(tx *bolt.Tx, e model.Entity) error {
	wanted := map[string]string{}
	for _, n := range e.Names() {
		wanted[model.Fold(n)] = n
	}
	return tx.Bucket(bucketEntities).ForEach(func(k, v []byte) error {
		if string(k) == e.ID {
			return nil
		}
		var other model.Entity
		if err := json.Unmarshal(v, &other); err != nil {
			return fmt.Errorf("decoding entity %s: %w", k, err)
		}
		if other.Type != e.Type {
			return nil
		}
		for _, n := range other.Names() {
			if name, ok := wanted[model.Fold(n)]; ok {
				return &NameTakenError{Name: name, Type: e.Type, Existing: other}
			}
		}
		return nil
	})
}

// linksToOtherEntities reports whether memoryID links to any entity besides
// entityID.
func linksToOtherEntities(tx *bolt.Tx, memoryID, entityID string) (bool, error) {
	links, err := memoryLinks(tx, memoryID)
	if err != nil {
		return false, err
	}
	for _, l := range links {
		if l.EntityID != entityID {
			return true, nil
		}
	}
	return false, nil
}
