package model

import "time"

// Entity is a thing memories can be about.
type Entity struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Aliases     []string   `json:"aliases"`
	Description string     `json:"description"`
	Type        EntityType `json:"type"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Names returns the entity's name followed by its aliases: everything it
// answers to.
func (e Entity) Names() []string {
	return append([]string{e.Name}, e.Aliases...)
}

// Memory is a titled piece of free text. It is only ever stored with at least
// one link.
type Memory struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Link attaches a memory to an entity. The triple is its identity.
type Link struct {
	MemoryID string   `json:"memory_id"`
	Type     LinkType `json:"type"`
	EntityID string   `json:"entity_id"`
}

// Edge relates two entities. The triple is its identity. Type is always the
// forward name, and a symmetric edge is stored with From < To so both
// orderings are the same edge.
type Edge struct {
	From string   `json:"from"`
	Type EdgeType `json:"type"`
	To   string   `json:"to"`
}

// Canonical returns the stored form of e: a symmetric edge with its ends
// ordered, any other edge unchanged.
func (e Edge) Canonical() Edge {
	if e.Type.Symmetric() && e.To < e.From {
		e.From, e.To = e.To, e.From
	}
	return e
}
