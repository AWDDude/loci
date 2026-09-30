package service

import (
	"github.com/AWDDude/loci/internal/model"
)

// LinkInput identifies a link by its triple.
type LinkInput struct {
	Memory string `json:"memory"`
	Type   string `json:"type"`
	Entity string `json:"entity"`
}

// LinkCreate links a memory to an entity. Creating a link that already
// exists is a no-op.
func (s *Service) LinkCreate(in LinkInput) (model.Link, error) {
	l, err := parseLinkInput(in)
	if err != nil {
		return model.Link{}, err
	}
	return s.store.CreateLink(l)
}

// LinkDelete removes a link. It is refused if it is the memory's last link.
func (s *Service) LinkDelete(in LinkInput) error {
	l, err := parseLinkInput(in)
	if err != nil {
		return err
	}
	return s.store.DeleteLink(l)
}

func parseLinkInput(in LinkInput) (model.Link, error) {
	memory, err := required("memory", in.Memory)
	if err != nil {
		return model.Link{}, err
	}
	l, err := parseLink(in.Type, in.Entity)
	l.MemoryID = memory
	return l, err
}

// parseLink validates a link's type and entity. The memory is left to the
// caller, since MemoryCreate has none until the store assigns it.
func parseLink(typ, entity string) (model.Link, error) {
	var l model.Link
	var err error
	if l.Type, err = model.ParseLinkType(typ); err != nil {
		return l, err
	}
	if l.EntityID, err = required("entity", entity); err != nil {
		return l, err
	}
	return l, nil
}

// EdgeInput identifies an edge by its triple. Type may be a forward name
// (parent_of) or an inverse one (child_of).
type EdgeInput struct {
	From string `json:"from"`
	Type string `json:"type"`
	To   string `json:"to"`
}

// EdgeCreate relates two entities and returns the edge as stored. Creating
// an edge that already exists is a no-op.
func (s *Service) EdgeCreate(in EdgeInput) (model.Edge, error) {
	e, err := parseEdge(in)
	if err != nil {
		return model.Edge{}, err
	}
	return s.store.CreateEdge(e)
}

// EdgeDelete removes an edge, given in either its forward or inverse form.
func (s *Service) EdgeDelete(in EdgeInput) error {
	e, err := parseEdge(in)
	if err != nil {
		return err
	}
	return s.store.DeleteEdge(e)
}

// parseEdge validates an edge triple and turns an inverse name into the
// stored forward form: "B child_of A" becomes "A parent_of B".
func parseEdge(in EdgeInput) (model.Edge, error) {
	var e model.Edge
	var err error
	if e.From, err = required("from", in.From); err != nil {
		return e, err
	}
	if e.To, err = required("to", in.To); err != nil {
		return e, err
	}
	var flipped bool
	if e.Type, flipped, err = model.ParseEdgeType(in.Type); err != nil {
		return e, err
	}
	if flipped {
		e.From, e.To = e.To, e.From
	}
	return e, nil
}
