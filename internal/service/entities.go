package service

import (
	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/store"
)

// EntitySearchInput is the input to EntitySearch.
type EntitySearchInput struct {
	Query string `json:"query"`
	// Type, if set, restricts results to one entity type.
	Type  string `json:"type"`
	Limit int    `json:"limit"`
}

// EntitySearchResult lists matching entities and the placeholder uuid that
// entity_create requires to make a new one instead.
type EntitySearchResult struct {
	Entities    []store.EntityHit `json:"entities"`
	Placeholder string            `json:"placeholder"`
}

// EntitySearch finds entities by name, alias and description, and issues a
// fresh entity placeholder, invalidating any earlier one.
func (s *Service) EntitySearch(in EntitySearchInput) (EntitySearchResult, error) {
	query, err := required("query", in.Query)
	if err != nil {
		return EntitySearchResult{}, err
	}
	var typ model.EntityType
	if in.Type != "" {
		if typ, err = model.ParseEntityType(in.Type); err != nil {
			return EntitySearchResult{}, err
		}
	}
	hits, err := s.store.SearchEntities(query, typ, limitOrDefault(in.Limit))
	if err != nil {
		return EntitySearchResult{}, err
	}
	p, err := s.store.NewPlaceholder(store.KindEntity)
	if err != nil {
		return EntitySearchResult{}, err
	}
	if hits == nil {
		hits = []store.EntityHit{}
	}
	return EntitySearchResult{Entities: hits, Placeholder: p}, nil
}

// EntityCreateInput is the input to EntityCreate.
type EntityCreateInput struct {
	Placeholder string   `json:"placeholder"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
}

// EntityCreate creates an entity. Placeholder must come from the latest
// entity search.
func (s *Service) EntityCreate(in EntityCreateInput) (model.Entity, error) {
	placeholder, err := required("placeholder", in.Placeholder)
	if err != nil {
		return model.Entity{}, err
	}
	e, err := normalizeEntity(in.Name, in.Aliases, in.Description, in.Type)
	if err != nil {
		return model.Entity{}, err
	}
	return s.store.CreateEntity(placeholder, e)
}

func normalizeEntity(name string, aliases []string, description, typ string) (model.Entity, error) {
	var e model.Entity
	var err error
	if e.Name, err = model.NormalizeLine("name", name); err != nil {
		return e, err
	}
	if e.Aliases, err = model.NormalizeAliases(e.Name, aliases); err != nil {
		return e, err
	}
	if e.Description, err = model.NormalizeLine("description", description); err != nil {
		return e, err
	}
	if e.Type, err = model.ParseEntityType(typ); err != nil {
		return e, err
	}
	return e, nil
}

// EdgeView is an edge as seen from the entity being viewed.
type EdgeView struct {
	// Name is the edge type read from this end, e.g. child_of when the stored
	// edge is the other entity's parent_of.
	Name  string    `json:"name"`
	Other EntityRef `json:"other"`
}

// MemoryTitle names a memory without its content.
type MemoryTitle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// EntityDetail is an entity with its neighborhood: its edges, and the titles
// of its linked memories grouped by link type.
type EntityDetail struct {
	Entity   model.Entity                     `json:"entity"`
	Edges    []EdgeView                       `json:"edges"`
	Memories map[model.LinkType][]MemoryTitle `json:"memories"`
}

// EntityGet returns an entity with its edges and linked memory titles.
func (s *Service) EntityGet(id string) (EntityDetail, error) {
	id, err := required("id", id)
	if err != nil {
		return EntityDetail{}, err
	}
	e, err := s.store.GetEntity(id)
	if err != nil {
		return EntityDetail{}, err
	}
	edges, err := s.store.EntityEdges(id)
	if err != nil {
		return EntityDetail{}, err
	}
	d := EntityDetail{Entity: e, Edges: make([]EdgeView, 0, len(edges)), Memories: map[model.LinkType][]MemoryTitle{}}
	for _, edge := range edges {
		other, err := s.entityRef(edge.Other)
		if err != nil {
			return EntityDetail{}, err
		}
		d.Edges = append(d.Edges, EdgeView{Name: edge.Name, Other: other})
	}
	linked, err := s.store.EntityMemories(id, "")
	if err != nil {
		return EntityDetail{}, err
	}
	for _, lm := range linked {
		d.Memories[lm.LinkType] = append(d.Memories[lm.LinkType], MemoryTitle{ID: lm.Memory.ID, Title: lm.Memory.Title})
	}
	return d, nil
}

// EntityUpdateInput is the input to EntityUpdate. A nil field is left
// unchanged; Aliases, when set, replaces the whole list.
type EntityUpdateInput struct {
	ID          string    `json:"id"`
	Name        *string   `json:"name"`
	Aliases     *[]string `json:"aliases"`
	Description *string   `json:"description"`
	Type        *string   `json:"type"`
}

// EntityUpdate edits an entity. Renaming does not keep the old name as an
// alias; the caller adds it if it still matters.
func (s *Service) EntityUpdate(in EntityUpdateInput) (model.Entity, error) {
	id, err := required("id", in.ID)
	if err != nil {
		return model.Entity{}, err
	}
	old, err := s.store.GetEntity(id)
	if err != nil {
		return model.Entity{}, err
	}
	name, aliases, description, typ := old.Name, old.Aliases, old.Description, string(old.Type)
	if in.Name != nil {
		name = *in.Name
	}
	if in.Aliases != nil {
		aliases = *in.Aliases
	}
	if in.Description != nil {
		description = *in.Description
	}
	if in.Type != nil {
		typ = *in.Type
	}
	e, err := normalizeEntity(name, aliases, description, typ)
	if err != nil {
		return model.Entity{}, err
	}
	e.ID = id
	return s.store.UpdateEntity(e)
}

// EntityDelete deletes an entity with its edges and links. It is refused if
// any memory links to no other entity.
func (s *Service) EntityDelete(id string) error {
	id, err := required("id", id)
	if err != nil {
		return err
	}
	return s.store.DeleteEntity(id)
}
