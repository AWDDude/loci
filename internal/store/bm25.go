package store

import (
	"math"
	"strings"
	"unicode"

	"github.com/AWDDude/loci/internal/model"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// tokenize folds text and splits it on anything that is not a letter or
// digit. There is no stemming and no stopword list, so distinctive exact
// tokens (identifiers, tool names, error strings) survive intact. It folds
// with model.Fold so that search agrees with the uniqueness check about what
// counts as the same word.
func tokenize(text string) []string {
	return strings.FieldsFunc(model.Fold(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// field is one piece of a document's text and how much each of its terms
// counts. A title term counting twice lets a title hit outweigh the same word
// buried in a body.
type field struct {
	text   string
	weight int
}

// bm25Index is an in-memory BM25 index. It is never persisted: the store
// rebuilds it from the records on open.
type bm25Index struct {
	// postings maps a term to the documents containing it and their weighted
	// term counts, so scoring walks only matching documents.
	postings map[string]map[string]int
	// terms maps a document to its distinct terms, so removal can clean up
	// its postings without scanning every term.
	terms    map[string][]string
	lengths  map[string]int
	totalLen int
}

func newBM25Index() *bm25Index {
	return &bm25Index{
		postings: map[string]map[string]int{},
		terms:    map[string][]string{},
		lengths:  map[string]int{},
	}
}

// set indexes, or re-indexes, one document.
func (ix *bm25Index) set(id string, fields ...field) {
	ix.remove(id)
	counts := map[string]int{}
	length := 0
	for _, f := range fields {
		for _, t := range tokenize(f.text) {
			counts[t] += f.weight
			length += f.weight
		}
	}
	terms := make([]string, 0, len(counts))
	for t, n := range counts {
		if ix.postings[t] == nil {
			ix.postings[t] = map[string]int{}
		}
		ix.postings[t][id] = n
		terms = append(terms, t)
	}
	ix.terms[id] = terms
	ix.lengths[id] = length
	ix.totalLen += length
}

func (ix *bm25Index) remove(id string) {
	terms, ok := ix.terms[id]
	if !ok {
		return
	}
	for _, t := range terms {
		delete(ix.postings[t], id)
		if len(ix.postings[t]) == 0 {
			delete(ix.postings, t)
		}
	}
	ix.totalLen -= ix.lengths[id]
	delete(ix.terms, id)
	delete(ix.lengths, id)
}

// score returns the BM25 score of every document matching at least one
// query term.
func (ix *bm25Index) score(query string) map[string]float64 {
	n := len(ix.terms)
	if n == 0 {
		return nil
	}
	avgLen := float64(ix.totalLen) / float64(n)
	scores := map[string]float64{}
	seen := map[string]bool{}
	for _, t := range tokenize(query) {
		if seen[t] {
			continue
		}
		seen[t] = true
		docs := ix.postings[t]
		if len(docs) == 0 {
			continue
		}
		df := float64(len(docs))
		idf := math.Log(1 + (float64(n)-df+0.5)/(df+0.5))
		for id, tf := range docs {
			norm := 1 - bm25B + bm25B*float64(ix.lengths[id])/avgLen
			scores[id] += idf * float64(tf) * (bm25K1 + 1) / (float64(tf) + bm25K1*norm)
		}
	}
	return scores
}
