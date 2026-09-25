package store

import (
	"errors"

	bolt "go.etcd.io/bbolt"
)

// Kind is a record kind that is created through a placeholder.
type Kind string

const (
	KindEntity Kind = "entity"
	KindMemory Kind = "memory"
)

// ErrStalePlaceholder rejects a create whose placeholder uuid is not the
// current one for its kind.
var ErrStalePlaceholder = errors.New("placeholder uuid is stale, search again")

func placeholderKey(kind Kind) []byte { return []byte("placeholder/" + string(kind)) }

// NewPlaceholder replaces kind's placeholder uuid and returns the new one.
// Every search calls it, which is what invalidates the placeholder from any
// earlier search.
func (s *Store) NewPlaceholder(kind Kind) (string, error) {
	id := newID()
	err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(placeholderKey(kind), []byte(id))
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// consumePlaceholder checks id against kind's current placeholder and rotates
// it. It runs inside the create's transaction, so a create that fails for any
// other reason rolls the rotation back and the same uuid can be retried.
func consumePlaceholder(tx *bolt.Tx, kind Kind, id string) error {
	meta := tx.Bucket(bucketMeta)
	current := meta.Get(placeholderKey(kind))
	if current == nil || string(current) != id {
		return ErrStalePlaceholder
	}
	return meta.Put(placeholderKey(kind), []byte(newID()))
}
