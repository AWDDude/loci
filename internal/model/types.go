// Package model defines Loci's records, the closed sets of types they use, and
// the rules for normalizing and comparing names. It has no storage or I/O.
package model

import (
	"fmt"
	"strings"
)

// EntityType classifies an entity.
type EntityType string

const (
	EntityPerson       EntityType = "person"
	EntityOrganization EntityType = "organization"
	EntityProject      EntityType = "project"
	EntityRepository   EntityType = "repository"
	EntityService      EntityType = "service"
	EntityTool         EntityType = "tool"
	EntityPlace        EntityType = "place"
	EntityConcept      EntityType = "concept"
)

// EntityTypes lists every entity type, in documentation order.
var EntityTypes = []EntityType{
	EntityPerson, EntityOrganization, EntityProject, EntityRepository,
	EntityService, EntityTool, EntityPlace, EntityConcept,
}

// ParseEntityType validates s against EntityTypes.
func ParseEntityType(s string) (EntityType, error) {
	for _, t := range EntityTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", unknownType("entity", s, EntityTypes)
}

// LinkType says how a memory relates to an entity it links to.
type LinkType string

const (
	LinkAttribute  LinkType = "attribute"
	LinkPreference LinkType = "preference"
	LinkEvent      LinkType = "event"
	LinkDecision   LinkType = "decision"
	LinkMention    LinkType = "mention"
)

// LinkTypes lists every link type, in documentation order.
var LinkTypes = []LinkType{LinkAttribute, LinkPreference, LinkEvent, LinkDecision, LinkMention}

// ParseLinkType validates s against LinkTypes.
func ParseLinkType(s string) (LinkType, error) {
	for _, t := range LinkTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", unknownType("link", s, LinkTypes)
}

// EdgeType is the stored, forward name of an entity-to-entity relationship.
type EdgeType string

const (
	EdgeParentOf  EdgeType = "parent_of"
	EdgeSpouseOf  EdgeType = "spouse_of"
	EdgeSiblingOf EdgeType = "sibling_of"
	EdgeMemberOf  EdgeType = "member_of"
	EdgeOwns      EdgeType = "owns"
	EdgeWorksOn   EdgeType = "works_on"
	EdgePartOf    EdgeType = "part_of"
	EdgeDependsOn EdgeType = "depends_on"
	EdgeUses      EdgeType = "uses"
	EdgeRelatedTo EdgeType = "related_to"
)

// EdgeTypes lists every forward edge type, in documentation order.
var EdgeTypes = []EdgeType{
	EdgeParentOf, EdgeSpouseOf, EdgeSiblingOf, EdgeMemberOf, EdgeOwns,
	EdgeWorksOn, EdgePartOf, EdgeDependsOn, EdgeUses, EdgeRelatedTo,
}

// edgeInverses maps each forward type to the name it has when read from the
// `to` end. A symmetric type is its own inverse.
var edgeInverses = map[EdgeType]string{
	EdgeParentOf:  "child_of",
	EdgeSpouseOf:  "spouse_of",
	EdgeSiblingOf: "sibling_of",
	EdgeMemberOf:  "has_member",
	EdgeOwns:      "owned_by",
	EdgeWorksOn:   "worked_on_by",
	EdgePartOf:    "has_part",
	EdgeDependsOn: "depended_on_by",
	EdgeUses:      "used_by",
	EdgeRelatedTo: "related_to",
}

// Inverse is the type's name as read from the `to` end.
func (t EdgeType) Inverse() string { return edgeInverses[t] }

// Symmetric reports whether the type reads the same from both ends.
func (t EdgeType) Symmetric() bool { return string(t) == edgeInverses[t] }

// ParseEdgeType accepts a forward or an inverse edge name. For an inverse name
// it returns the forward type with flipped set, meaning the caller's from and
// to must be swapped: "B child_of A" is stored as "A parent_of B". This lets a
// caller use the name entity_get showed it, from whichever end it was reading.
func ParseEdgeType(s string) (t EdgeType, flipped bool, err error) {
	for _, t := range EdgeTypes {
		if string(t) == s {
			return t, false, nil
		}
		if t.Inverse() == s {
			return t, true, nil
		}
	}
	return "", false, fmt.Errorf("unknown edge type %q (valid: %s)", s, strings.Join(EdgeTypeNames(), ", "))
}

// EdgeTypeNames lists every name ParseEdgeType accepts: each forward type
// followed by its inverse, when it has a distinct one.
func EdgeTypeNames() []string {
	names := make([]string, 0, 2*len(EdgeTypes))
	for _, t := range EdgeTypes {
		names = append(names, string(t))
		if !t.Symmetric() {
			names = append(names, t.Inverse())
		}
	}
	return names
}

func unknownType[T ~string](kind, s string, valid []T) error {
	names := make([]string, len(valid))
	for i, t := range valid {
		names[i] = string(t)
	}
	return fmt.Errorf("unknown %s type %q (valid: %s)", kind, s, strings.Join(names, ", "))
}
