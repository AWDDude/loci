package model

import (
	"slices"
	"strings"
	"testing"
)

func TestParseEntityTypeListsValidOnError(t *testing.T) {
	if got, err := ParseEntityType("tool"); err != nil || got != EntityTool {
		t.Fatalf("ParseEntityType(tool) = %q, %v", got, err)
	}
	_, err := ParseEntityType("Person")
	if err == nil || !strings.Contains(err.Error(), "person, organization") {
		t.Fatalf("want error listing valid types, got %v", err)
	}
}

func TestParseLinkType(t *testing.T) {
	if got, err := ParseLinkType("decision"); err != nil || got != LinkDecision {
		t.Fatalf("ParseLinkType(decision) = %q, %v", got, err)
	}
	if _, err := ParseLinkType("tag"); err == nil {
		t.Fatal("want error for unknown link type")
	}
}

func TestParseEdgeType(t *testing.T) {
	tests := []struct {
		in      string
		want    EdgeType
		flipped bool
	}{
		{"parent_of", EdgeParentOf, false},
		{"child_of", EdgeParentOf, true},
		{"used_by", EdgeUses, true},
		{"spouse_of", EdgeSpouseOf, false},
	}
	for _, tt := range tests {
		got, flipped, err := ParseEdgeType(tt.in)
		if err != nil || got != tt.want || flipped != tt.flipped {
			t.Errorf("ParseEdgeType(%q) = %q, %v, %v; want %q, %v", tt.in, got, flipped, err, tt.want, tt.flipped)
		}
	}
	_, _, err := ParseEdgeType("likes")
	if err == nil || !strings.Contains(err.Error(), "child_of") {
		t.Fatalf("want error listing forward and inverse names, got %v", err)
	}
}

func TestEveryEdgeTypeHasInverse(t *testing.T) {
	for _, et := range EdgeTypes {
		if et.Inverse() == "" {
			t.Errorf("%s has no inverse", et)
		}
	}
}

func TestEdgeCanonical(t *testing.T) {
	sym := Edge{From: "b", Type: EdgeSpouseOf, To: "a"}
	if got := sym.Canonical(); got.From != "a" || got.To != "b" {
		t.Errorf("symmetric edge not ordered: %+v", got)
	}
	dir := Edge{From: "b", Type: EdgeParentOf, To: "a"}
	if got := dir.Canonical(); got != dir {
		t.Errorf("directional edge changed: %+v", got)
	}
}

func TestFold(t *testing.T) {
	if Fold("engRam") != Fold("ENGRAM") {
		t.Error("case variants should fold equal")
	}
	if Fold("Straße") != Fold("STRASSE") {
		t.Error("full Unicode folding should equate ß and SS")
	}
}

func TestNormalizeLine(t *testing.T) {
	got, err := NormalizeLine("name", "  David \t  Kittle ")
	if err != nil || got != "David Kittle" {
		t.Fatalf("NormalizeLine = %q, %v", got, err)
	}
	if got, _ := NormalizeLine("name", "engRam"); got != "engRam" {
		t.Errorf("casing changed: %q", got)
	}
	for _, bad := range []string{"", "   ", "two\nlines", "cr\rhere", "bell\a"} {
		if _, err := NormalizeLine("name", bad); err == nil {
			t.Errorf("NormalizeLine(%q) accepted, want error", bad)
		}
	}
}

func TestNormalizeAliasesDropsDuplicates(t *testing.T) {
	got, err := NormalizeAliases("David", []string{"dave", " DAVID ", "Dave", "dk"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dave", "dk"}; !slices.Equal(got, want) {
		t.Errorf("aliases = %q, want %q", got, want)
	}
}

func TestNormalizeContent(t *testing.T) {
	got, err := NormalizeContent("\n line one\n\nline two  \n")
	if err != nil || got != "line one\n\nline two" {
		t.Fatalf("NormalizeContent = %q, %v", got, err)
	}
	if _, err := NormalizeContent(" \n "); err == nil {
		t.Error("want error for blank content")
	}
}
