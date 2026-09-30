package service

import (
	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/store"
)

// MemorySearchInput is the input to MemorySearch.
type MemorySearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// MemorySearchResult lists matching memories and the placeholder uuid that
// memory_create requires to make a new one instead.
type MemorySearchResult struct {
	Memories    []store.MemoryHit `json:"memories"`
	Placeholder string            `json:"placeholder"`
}

// MemorySearch finds memories by title and content, and issues a fresh memory
// placeholder, invalidating any earlier one.
func (s *Service) MemorySearch(in MemorySearchInput) (MemorySearchResult, error) {
	query, err := required("query", in.Query)
	if err != nil {
		return MemorySearchResult{}, err
	}
	hits, err := s.store.SearchMemories(query, limitOrDefault(in.Limit))
	if err != nil {
		return MemorySearchResult{}, err
	}
	p, err := s.store.NewPlaceholder(store.KindMemory)
	if err != nil {
		return MemorySearchResult{}, err
	}
	if hits == nil {
		hits = []store.MemoryHit{}
	}
	return MemorySearchResult{Memories: hits, Placeholder: p}, nil
}

// LinkSpec is one link given when creating a memory.
type LinkSpec struct {
	Type   string `json:"type"`
	Entity string `json:"entity"`
}

// MemoryCreateInput is the input to MemoryCreate.
type MemoryCreateInput struct {
	Placeholder string     `json:"placeholder"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Links       []LinkSpec `json:"links"`
}

// LinkView is one of a memory's links, with the entity it points to.
type LinkView struct {
	Type   model.LinkType `json:"type"`
	Entity EntityRef      `json:"entity"`
}

// MemoryDetail is a memory with its links.
type MemoryDetail struct {
	Memory model.Memory `json:"memory"`
	Links  []LinkView   `json:"links"`
}

// MemoryCreate creates a memory linked to at least one entity. Placeholder
// must come from the latest memory search.
func (s *Service) MemoryCreate(in MemoryCreateInput) (MemoryDetail, error) {
	placeholder, err := required("placeholder", in.Placeholder)
	if err != nil {
		return MemoryDetail{}, err
	}
	var m model.Memory
	if m.Title, err = model.NormalizeLine("title", in.Title); err != nil {
		return MemoryDetail{}, err
	}
	if m.Content, err = model.NormalizeContent(in.Content); err != nil {
		return MemoryDetail{}, err
	}
	links := make([]model.Link, len(in.Links))
	for i, spec := range in.Links {
		if links[i], err = parseLink(spec.Type, spec.Entity); err != nil {
			return MemoryDetail{}, err
		}
	}
	m, stored, err := s.store.CreateMemory(placeholder, m, links)
	if err != nil {
		return MemoryDetail{}, err
	}
	return s.memoryDetail(m, stored)
}

// MemoryGet returns a memory with its links.
func (s *Service) MemoryGet(id string) (MemoryDetail, error) {
	id, err := required("id", id)
	if err != nil {
		return MemoryDetail{}, err
	}
	m, links, err := s.store.GetMemory(id)
	if err != nil {
		return MemoryDetail{}, err
	}
	return s.memoryDetail(m, links)
}

func (s *Service) memoryDetail(m model.Memory, links []model.Link) (MemoryDetail, error) {
	d := MemoryDetail{Memory: m, Links: make([]LinkView, len(links))}
	for i, l := range links {
		ref, err := s.entityRef(l.EntityID)
		if err != nil {
			return MemoryDetail{}, err
		}
		d.Links[i] = LinkView{Type: l.Type, Entity: ref}
	}
	return d, nil
}

// MemoryUpdateInput is the input to MemoryUpdate. A nil field is left
// unchanged.
type MemoryUpdateInput struct {
	ID      string  `json:"id"`
	Title   *string `json:"title"`
	Content *string `json:"content"`
}

// MemoryUpdate edits a memory's title or content. Links are changed with
// LinkCreate and LinkDelete.
func (s *Service) MemoryUpdate(in MemoryUpdateInput) (model.Memory, error) {
	id, err := required("id", in.ID)
	if err != nil {
		return model.Memory{}, err
	}
	m, _, err := s.store.GetMemory(id)
	if err != nil {
		return model.Memory{}, err
	}
	if in.Title != nil {
		if m.Title, err = model.NormalizeLine("title", *in.Title); err != nil {
			return model.Memory{}, err
		}
	}
	if in.Content != nil {
		if m.Content, err = model.NormalizeContent(*in.Content); err != nil {
			return model.Memory{}, err
		}
	}
	return s.store.UpdateMemory(m)
}

// MemoryDelete deletes a memory and its links.
func (s *Service) MemoryDelete(id string) error {
	id, err := required("id", id)
	if err != nil {
		return err
	}
	return s.store.DeleteMemory(id)
}

// MemoryListInput is the input to MemoryList.
type MemoryListInput struct {
	Entity string `json:"entity"`
	// LinkType, if set, restricts results to memories linked by that type.
	LinkType string `json:"link_type"`
}

// MemoryList returns the full memories linked to an entity, newest first.
func (s *Service) MemoryList(in MemoryListInput) ([]store.LinkedMemory, error) {
	entity, err := required("entity", in.Entity)
	if err != nil {
		return nil, err
	}
	var lt model.LinkType
	if in.LinkType != "" {
		if lt, err = model.ParseLinkType(in.LinkType); err != nil {
			return nil, err
		}
	}
	out, err := s.store.EntityMemories(entity, lt)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []store.LinkedMemory{}
	}
	return out, nil
}
