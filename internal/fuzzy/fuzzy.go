// Package fuzzy implements the subsequence scoring used by the global
// resource finder (AXE-013). It is dependency-free and deterministic: the
// query matches when its characters appear in order in the target, and the
// score rewards the alignments humans mean — consecutive runs, word starts,
// and exact substrings — so "eni0abc" finds "eni-0abc12" and "prodweb" finds
// "prod-web-3".
package fuzzy

import (
	"sort"
	"strings"
)

// Scoring weights. Relative order matters more than absolute values: an
// exact substring beats any scattered subsequence, a consecutive run beats
// gaps, and earlier/word-start matches beat mid-word ones.
const (
	matchScore       = 4  // per matched character
	consecutiveBonus = 6  // per matched character directly after the previous match
	wordStartBonus   = 8  // match at index 0 or after a separator
	substringBonus   = 40 // the whole query appears verbatim
	gapPenalty       = 1  // per skipped target character between matches
)

// Score rates how well query matches target. ok is false when query is not a
// case-insensitive subsequence of target (a score of 0 with ok=true is
// possible for an empty query, which matches everything equally).
func Score(query, target string) (score int, ok bool) {
	q := strings.ToLower(query)
	t := strings.ToLower(target)
	if q == "" {
		return 0, true
	}
	if t == "" {
		return 0, false
	}

	if strings.Contains(t, q) {
		score += substringBonus
		// Fall through to the positional walk so shorter targets and
		// word-start hits still rank higher among substring matches.
	}

	qi := 0
	prevMatch := -2 // not adjacent to index 0
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if t[ti] != q[qi] {
			if qi > 0 {
				score -= gapPenalty
			}
			continue
		}
		score += matchScore
		if ti == prevMatch+1 {
			score += consecutiveBonus
		}
		if ti == 0 || isSeparator(t[ti-1]) {
			score += wordStartBonus
		}
		prevMatch = ti
		qi++
	}
	if qi < len(q) {
		return 0, false
	}

	// Among equal alignments, prefer the shorter target ("vpc-1" over
	// "vpc-1-subnet-a" for query "vpc1").
	score -= len(t) / 8
	return score, true
}

func isSeparator(c byte) bool {
	switch c {
	case '-', '_', '.', '/', ':', ' ', '\t':
		return true
	}
	return false
}

// Hit is one ranked match.
type Hit struct {
	Index int // index into the candidates slice handed to Rank
	Score int
}

// Rank scores every candidate against the query and returns the matching
// indices, best first, capped at limit (0 = no cap). Ties keep candidate
// order, so pre-sorted inputs stay stable.
func Rank(query string, candidates []string, limit int) []Hit {
	hits := make([]Hit, 0, 64)
	for i, c := range candidates {
		if s, ok := Score(query, c); ok {
			hits = append(hits, Hit{Index: i, Score: s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
