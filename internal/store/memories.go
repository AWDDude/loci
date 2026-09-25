package store

import (
	"errors"
	"sort"

	bolt "go.etcd.io/bbolt"

	"github.com/AWDDude/loci/internal/model"
)

// ErrNoLinks rejects a memory created without any link.
var ErrNoLinks = errors.New("a memory must link to at least one entity")

// CreateMemory stores m under a new uuid together with its links. placeholder
// must be the current memory placeholder, and links must be non-empty. Each
// link's MemoryID is ignored and set to the new uuid; duplicate links collapse
// into one.
func (s *Store) CreateMemory(placeholder string, m model.Memory, links []model.Link) (model.Memory, []model.Link, error) {
	if len(links) == 0 {
		return model.Memory{}, nil, ErrNoLinks
	}
	var stored []model.Link
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := consumePlaceholder(tx, KindMemory, placeholder); err != nil {
			return err
		}
		m.ID = newID()
		m.CreatedAt = s.now()
		m.UpdatedAt = m.CreatedAt
		if err := putJSON(tx.Bucket(bucketMemories), m.ID, m); err != nil {
			return err
		}
		for _, l := range links {
			l.MemoryID = m.ID
			if err := putLink(tx, l); err != nil {
				return err
			}
		}
		var err error
		stored, err = memoryLinks(tx, m.ID)
		return err
	})
	if err != nil {
		return model.Memory{}, nil, err
	}
	return m, stored, nil
}

// GetMemory returns the memory with the given uuid and its links.
func (s *Store) GetMemory(id string) (model.Memory, []model.Link, error) {
	var m model.Memory
	var links []model.Link
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := getJSON(tx.Bucket(bucketMemories), "memory", id, &m); err != nil {
			return err
		}
		var err error
		links, err = memoryLinks(tx, id)
		return err
	})
	if err != nil {
		return model.Memory{}, nil, err
	}
	return m, links, nil
}

// UpdateMemory replaces the title and content of the memory with m.ID.
func (s *Store) UpdateMemory(m model.Memory) (model.Memory, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMemories)
		var old model.Memory
		if err := getJSON(b, "memory", m.ID, &old); err != nil {
			return err
		}
		m.CreatedAt = old.CreatedAt
		m.UpdatedAt = s.now()
		return putJSON(b, m.ID, m)
	})
	if err != nil {
		return model.Memory{}, err
	}
	return m, nil
}

// DeleteMemory removes the memory and its links.
func (s *Store) DeleteMemory(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMemories)
		if b.Get([]byte(id)) == nil {
			return notFound("memory", id)
		}
		links, err := memoryLinks(tx, id)
		if err != nil {
			return err
		}
		for _, l := range links {
			if err := deleteLink(tx, l); err != nil {
				return err
			}
		}
		return b.Delete([]byte(id))
	})
}

// LinkedMemory is a memory as reached through one of its links to an entity.
// A memory linked to the same entity under two types appears once per type.
type LinkedMemory struct {
	Memory   model.Memory   `json:"memory"`
	LinkType model.LinkType `json:"link_type"`
}

// EntityMemories returns the memories linked to an entity, newest first. An
// empty linkType returns every link type.
func (s *Store) EntityMemories(entityID string, linkType model.LinkType) ([]LinkedMemory, error) {
	var out []LinkedMemory
	err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketEntities).Get([]byte(entityID)) == nil {
			return notFound("entity", entityID)
		}
		links, err := entityLinks(tx, entityID)
		if err != nil {
			return err
		}
		for _, l := range links {
			if linkType != "" && l.Type != linkType {
				continue
			}
			var m model.Memory
			if err := getJSON(tx.Bucket(bucketMemories), "memory", l.MemoryID, &m); err != nil {
				return err
			}
			out = append(out, LinkedMemory{Memory: m, LinkType: l.Type})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Memory.UpdatedAt.After(out[j].Memory.UpdatedAt)
	})
	return out, nil
}
