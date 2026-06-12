package fuzzy

import "testing"

func TestScoreMatching(t *testing.T) {
	cases := []struct {
		query, target string
		want          bool
	}{
		{"", "anything", true},
		{"eni0abc", "eni-0abc12def", true},  // separators skipped
		{"ENI-0ABC", "eni-0abc12def", true}, // case-insensitive
		{"prodweb", "prod-web-3", true},     // word starts
		{"vpc1", "vpc-1", true},
		{"xyz", "vpc-1", false}, // not a subsequence
		{"abc", "", false},
		{"cba", "abc", false}, // order matters
	}
	for _, c := range cases {
		if _, ok := Score(c.query, c.target); ok != c.want {
			t.Errorf("Score(%q, %q) ok = %v, want %v", c.query, c.target, ok, c.want)
		}
	}
}

func TestScoreRanking(t *testing.T) {
	// An exact substring must outrank a scattered subsequence.
	sub, _ := Score("web", "prod-web-3")
	scattered, _ := Score("web", "w1e2b3-something")
	if sub <= scattered {
		t.Errorf("substring (%d) should outrank scattered (%d)", sub, scattered)
	}

	// A shorter target wins among equal alignments.
	short, _ := Score("vpc1", "vpc-1")
	long, _ := Score("vpc1", "vpc-1-subnet-with-long-name")
	if short <= long {
		t.Errorf("shorter target (%d) should outrank longer (%d)", short, long)
	}

	// Word-start matches beat mid-word matches.
	wordStart, _ := Score("web", "prod-web")
	midWord, _ := Score("web", "spiderweb")
	if wordStart <= midWord {
		t.Errorf("word-start (%d) should outrank mid-word (%d)", wordStart, midWord)
	}
}

func TestRank(t *testing.T) {
	candidates := []string{
		"i-0abc (prod-web-1)",
		"vpc-1 main",
		"prod-web-bucket",
		"unrelated-thing",
	}
	hits := Rank("prodweb", candidates, 0)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2 (%+v)", len(hits), hits)
	}
	// Both contain "prod-web"; candidate order breaks the tie when scores
	// are equal, and either way the unrelated entries must be absent.
	for _, h := range hits {
		if candidates[h.Index] == "unrelated-thing" || candidates[h.Index] == "vpc-1 main" {
			t.Errorf("unexpected hit: %q", candidates[h.Index])
		}
	}
}

func TestRankEmptyQueryAndLimit(t *testing.T) {
	candidates := []string{"a", "b", "c"}
	hits := Rank("", candidates, 2)
	if len(hits) != 2 {
		t.Fatalf("limit not applied: %d hits", len(hits))
	}
	// Empty query keeps candidate order.
	if hits[0].Index != 0 || hits[1].Index != 1 {
		t.Errorf("empty query should keep order: %+v", hits)
	}
}
