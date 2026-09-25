// Package store persists Loci's records in a single bbolt file and enforces
// the invariants that span records. Every exported operation is one bbolt
// transaction, so an invariant check and the write it guards cannot be split
// by another writer.
//
// Callers pass values already normalized and validated by package model; the
// store checks relationships (existence, uniqueness, last links, placeholders),
// not formatting.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// schemaVersion is bumped whenever the bucket layout or record encoding
// changes in a way an older build could not read.
const schemaVersion = "1"

// openTimeout bounds the wait for bbolt's file lock. Only the daemon opens the
// file, so a lock held for longer than this is another daemon, not a slow one.
const openTimeout = time.Second

var (
	bucketEntities = []byte("entities") // uuid → JSON entity
	bucketMemories = []byte("memories") // uuid → JSON memory

	// Edges and links are pure keys: the triple is the record, so the value is
	// empty. Each is stored twice, once per end, so either end can be listed
	// with a prefix scan.
	bucketEdges         = []byte("edges")           // from/type/to
	bucketEdgesByTo     = []byte("edges_by_to")     // to/type/from
	bucketLinks         = []byte("links")           // memory/type/entity
	bucketLinksByEntity = []byte("links_by_entity") // entity/type/memory

	// bucketMeta holds the schema version and the placeholder uuids. Keeping
	// placeholders here rather than as records is what keeps them out of every
	// list and count.
	bucketMeta = []byte("meta")

	allBuckets = [][]byte{
		bucketEntities, bucketMemories, bucketEdges, bucketEdgesByTo,
		bucketLinks, bucketLinksByEntity, bucketMeta,
	}

	metaSchemaVersion = []byte("schema_version")
)

// Store is an open Loci database.
type Store struct {
	db  *bolt.DB
	now func() time.Time
}

// Open opens or creates the database at path, creating its directory if
// needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("creating bucket %s: %w", name, err)
			}
		}
		meta := tx.Bucket(bucketMeta)
		switch v := meta.Get(metaSchemaVersion); {
		case v == nil:
			return meta.Put(metaSchemaVersion, []byte(schemaVersion))
		case string(v) != schemaVersion:
			return fmt.Errorf("database schema version %s, this build reads %s", v, schemaVersion)
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Close releases the database file.
func (s *Store) Close() error { return s.db.Close() }

// ErrNotFound is wrapped by every error for a uuid or triple that does not
// exist.
var ErrNotFound = errors.New("not found")

func notFound(kind, id string) error {
	return fmt.Errorf("%s %s: %w", kind, id, ErrNotFound)
}

// key joins parts with "/". Uuids and type names never contain one, so a key
// splits back into exactly the parts it was built from.
func key(parts ...string) []byte {
	return []byte(strings.Join(parts, "/"))
}

// scanPrefix calls fn with the parts of every key in b under prefix/.
func scanPrefix(b *bolt.Bucket, prefix string, fn func(parts []string) error) error {
	p := []byte(prefix + "/")
	c := b.Cursor()
	for k, _ := c.Seek(p); k != nil && bytes.HasPrefix(k, p); k, _ = c.Next() {
		if err := fn(strings.Split(string(k), "/")); err != nil {
			return err
		}
	}
	return nil
}

// getJSON decodes the record stored under id, or returns ErrNotFound.
func getJSON(b *bolt.Bucket, kind, id string, v any) error {
	data := b.Get([]byte(id))
	if data == nil {
		return notFound(kind, id)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding %s %s: %w", kind, id, err)
	}
	return nil
}

func putJSON(b *bolt.Bucket, id string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.Put([]byte(id), data)
}

func newID() string { return uuid.NewString() }
