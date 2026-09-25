package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
)

// Fold is the one definition of "same name": two strings name the same thing
// when their folds are equal. Search and uniqueness checks both call it, and
// its result is never stored, since a stored key could drift from the display
// name it was derived from.
func Fold(s string) string {
	// A Caser carries state and is not safe for concurrent use, so each call
	// gets its own.
	return cases.Fold().String(s)
}

// NormalizeLine prepares a single-line field (a name, alias, description or
// title) for storage: it trims the ends and collapses every run of whitespace
// to one space. Casing is kept as given. Newlines and other control characters
// are rejected rather than collapsed, since they signal pasted content, not a
// line. field names the input in the error.
func NormalizeLine(field, s string) (string, error) {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return "", fmt.Errorf("%s must be a single line", field)
		}
		if unicode.IsControl(r) && r != '\t' {
			return "", fmt.Errorf("%s contains control character %U", field, r)
		}
	}
	out := strings.Join(strings.Fields(s), " ")
	if out == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	return out, nil
}

// NormalizeAliases normalizes each alias and drops any that fold equal to the
// name or to an earlier alias, so an entity never answers to one name twice.
func NormalizeAliases(name string, aliases []string) ([]string, error) {
	seen := map[string]bool{Fold(name): true}
	out := make([]string, 0, len(aliases))
	for _, a := range aliases {
		a, err := NormalizeLine("alias", a)
		if err != nil {
			return nil, err
		}
		if k := Fold(a); !seen[k] {
			seen[k] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// NormalizeContent trims a memory's content. Unlike a line it keeps its inner
// whitespace and newlines.
func NormalizeContent(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("content must not be empty")
	}
	return s, nil
}
