package store

import (
	"errors"

	bolt "go.etcd.io/bbolt"

	"github.com/AWDDude/loci/internal/model"
)

// ErrLastLink refuses to remove a memory's only link.
var ErrLastLink = errors.New("cannot remove a memory's last link; link it elsewhere first or delete the memory")

// ErrSelfEdge rejects an edge from an entity to itself.
var ErrSelfEdge = errors.New("an edge must join two different entities")

// CreateLink stores l. Creating a link that already exists is a no-op.
func (s *Store) CreateLink(l model.Link) (model.Link, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketMemories).Get([]byte(l.MemoryID)) == nil {
			return notFound("memory", l.MemoryID)
		}
		return putLink(tx, l)
	})
	if err != nil {
		return model.Link{}, err
	}
	return l, nil
}

// DeleteLink removes l. It is refused with ErrLastLink if l is its memory's
// only link.
func (s *Store) DeleteLink(l model.Link) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketLinks).Get(key(l.MemoryID, string(l.Type), l.EntityID)) == nil {
			return notFound("link", string(key(l.MemoryID, string(l.Type), l.EntityID)))
		}
		links, err := memoryLinks(tx, l.MemoryID)
		if err != nil {
			return err
		}
		if len(links) == 1 {
			return ErrLastLink
		}
		return deleteLink(tx, l)
	})
}

// putLink writes both keys of l after checking that its entity exists.
func putLink(tx *bolt.Tx, l model.Link) error {
	if tx.Bucket(bucketEntities).Get([]byte(l.EntityID)) == nil {
		return notFound("entity", l.EntityID)
	}
	if err := tx.Bucket(bucketLinks).Put(key(l.MemoryID, string(l.Type), l.EntityID), nil); err != nil {
		return err
	}
	return tx.Bucket(bucketLinksByEntity).Put(key(l.EntityID, string(l.Type), l.MemoryID), nil)
}

func deleteLink(tx *bolt.Tx, l model.Link) error {
	if err := tx.Bucket(bucketLinks).Delete(key(l.MemoryID, string(l.Type), l.EntityID)); err != nil {
		return err
	}
	return tx.Bucket(bucketLinksByEntity).Delete(key(l.EntityID, string(l.Type), l.MemoryID))
}

// memoryLinks returns a memory's links in key order.
func memoryLinks(tx *bolt.Tx, memoryID string) ([]model.Link, error) {
	var out []model.Link
	err := scanPrefix(tx.Bucket(bucketLinks), memoryID, func(p []string) error {
		out = append(out, model.Link{MemoryID: p[0], Type: model.LinkType(p[1]), EntityID: p[2]})
		return nil
	})
	return out, err
}

// entityLinks returns the links pointing at an entity in key order.
func entityLinks(tx *bolt.Tx, entityID string) ([]model.Link, error) {
	var out []model.Link
	err := scanPrefix(tx.Bucket(bucketLinksByEntity), entityID, func(p []string) error {
		out = append(out, model.Link{EntityID: p[0], Type: model.LinkType(p[1]), MemoryID: p[2]})
		return nil
	})
	return out, err
}

// CreateEdge stores e in canonical form and returns that form. Creating an
// edge that already exists, in either order for a symmetric type, is a no-op.
func (s *Store) CreateEdge(e model.Edge) (model.Edge, error) {
	e = e.Canonical()
	if e.From == e.To {
		return model.Edge{}, ErrSelfEdge
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		entities := tx.Bucket(bucketEntities)
		for _, id := range []string{e.From, e.To} {
			if entities.Get([]byte(id)) == nil {
				return notFound("entity", id)
			}
		}
		if err := tx.Bucket(bucketEdges).Put(key(e.From, string(e.Type), e.To), nil); err != nil {
			return err
		}
		return tx.Bucket(bucketEdgesByTo).Put(key(e.To, string(e.Type), e.From), nil)
	})
	if err != nil {
		return model.Edge{}, err
	}
	return e, nil
}

// DeleteEdge removes e, matching a symmetric edge in either order.
func (s *Store) DeleteEdge(e model.Edge) error {
	e = e.Canonical()
	return s.db.Update(func(tx *bolt.Tx) error {
		k := key(e.From, string(e.Type), e.To)
		if tx.Bucket(bucketEdges).Get(k) == nil {
			return notFound("edge", string(k))
		}
		return deleteEdge(tx, e)
	})
}

func deleteEdge(tx *bolt.Tx, e model.Edge) error {
	if err := tx.Bucket(bucketEdges).Delete(key(e.From, string(e.Type), e.To)); err != nil {
		return err
	}
	return tx.Bucket(bucketEdgesByTo).Delete(key(e.To, string(e.Type), e.From))
}

// EntityEdge is an edge as seen from one of its ends.
type EntityEdge struct {
	Edge model.Edge `json:"edge"`
	// Name is the edge's type as read from this end: the forward name when
	// this entity is From, the inverse name when it is To.
	Name string `json:"name"`
	// Other is the uuid of the entity at the far end.
	Other string `json:"other"`
}

// EntityEdges returns every edge touching an entity, outgoing first.
func (s *Store) EntityEdges(entityID string) ([]EntityEdge, error) {
	var out []EntityEdge
	err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketEntities).Get([]byte(entityID)) == nil {
			return notFound("entity", entityID)
		}
		var err error
		out, err = entityEdges(tx, entityID)
		return err
	})
	return out, err
}

func entityEdges(tx *bolt.Tx, entityID string) ([]EntityEdge, error) {
	var out []EntityEdge
	err := scanPrefix(tx.Bucket(bucketEdges), entityID, func(p []string) error {
		t := model.EdgeType(p[1])
		out = append(out, EntityEdge{Edge: model.Edge{From: p[0], Type: t, To: p[2]}, Name: string(t), Other: p[2]})
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = scanPrefix(tx.Bucket(bucketEdgesByTo), entityID, func(p []string) error {
		t := model.EdgeType(p[1])
		out = append(out, EntityEdge{Edge: model.Edge{From: p[2], Type: t, To: p[0]}, Name: t.Inverse(), Other: p[2]})
		return nil
	})
	return out, err
}
