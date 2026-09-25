package store

import (
	"strings"
	"unicode"

	"github.com/AWDDude/loci/internal/model"
)

// trigramThreshold is the least similarity that counts as a match. It is
// pg_trgm's default, which lets "davd" find "David" while keeping unrelated
// short names out.
const trigramThreshold = 0.3

// trigrams returns the set of trigrams of s in the pg_trgm style: s is folded,
// split into words on anything that is not a letter or digit, and each word is
// padded with two spaces in front and one behind, so a word's start weighs
// more than its middle.
func trigrams(s string) map[string]struct{} {
	out := map[string]struct{}{}
	words := strings.FieldsFunc(model.Fold(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		r := []rune("  " + w + " ")
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = struct{}{}
		}
	}
	return out
}

// nameTrigrams returns the trigram sets a name is matched against: the whole
// name, plus each of its words when it has several. Jaccard similarity over a
// whole multi-word name is diluted by the words the query did not mention, so
// "davd" would miss "David Kittle"; matching words separately is the same
// idea as pg_trgm's word_similarity.
func nameTrigrams(name string) []map[string]struct{} {
	sets := []map[string]struct{}{trigrams(name)}
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > 1 {
		for _, w := range words {
			sets = append(sets, trigrams(w))
		}
	}
	return sets
}

// similarity is the Jaccard similarity of two trigram sets: shared trigrams
// over all distinct trigrams.
func similarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for t := range a {
		if _, ok := b[t]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
