// Package service implements every Loci capability exactly once. The MCP tools
// and, through them, the CLI commands are thin adapters over these methods, so
// this is where input is parsed, normalized and validated before it reaches
// the store.
//
// Inputs take raw strings as they arrive from a caller; results are plain
// structs that serialize to the JSON both surfaces return.
package service

import (
	"errors"
	"strings"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/store"
)

// DefaultLimit caps search results when the caller gives no limit.
const DefaultLimit = 20

// Service is the set of Loci capabilities over one store.
type Service struct {
	store *store.Store
}

// New returns a Service over s.
func New(s *store.Store) *Service {
	return &Service{store: s}
}

// EntityRef identifies an entity inside another result.
type EntityRef struct {
	ID   string           `json:"id"`
	Name string           `json:"name"`
	Type model.EntityType `json:"type"`
}

func refOf(e model.Entity) EntityRef {
	return EntityRef{ID: e.ID, Name: e.Name, Type: e.Type}
}

// entityRef looks up an entity for display inside another result.
func (s *Service) entityRef(id string) (EntityRef, error) {
	e, err := s.store.GetEntity(id)
	if err != nil {
		return EntityRef{}, err
	}
	return refOf(e), nil
}

// required trims a uuid or query argument and rejects it if empty.
func required(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New(field + " is required")
	}
	return s, nil
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	return limit
}
