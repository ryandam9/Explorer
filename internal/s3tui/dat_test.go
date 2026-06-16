package s3tui

import (
	"strings"
	"testing"
)

func TestDatExtensionRoutesToTable(t *testing.T) {
	for _, k := range []string{"abc.dat", "EXTRACT.DAT", "data/feed.psv"} {
		if !looksLikeCSV(k) {
			t.Errorf("%q should route to the CSV table view", k)
		}
	}
}

func TestDetectUnitSeparator(t *testing.T) {
	us := "\x1f"
	content := strings.Join([]string{
		"id" + us + "name" + us + "amount",
		"1" + us + "alice" + us + "100",
		"2" + us + "bob" + us + "200",
	}, "\n")
	if d := detectDelimiter(content); d != '\x1f' {
		t.Fatalf("detected delimiter = %q, want ASCII 31 unit separator", string(d))
	}
	recs, ok := parseCSV(content, '\x1f')
	if !ok || len(recs) != 3 || len(recs[0]) != 3 || recs[1][1] != "alice" {
		t.Errorf("US-delimited parse wrong: ok=%v recs=%v", ok, recs)
	}
}

// Adding \x1f must not change detection for the common text delimiters.
func TestDetectStillPrefersCommonDelimiters(t *testing.T) {
	cases := map[string]rune{
		"a,b,c\n1,2,3":     ',',
		"a\tb\tc\n1\t2\t3": '\t',
		"a|b|c\n1|2|3":     '|',
	}
	for content, want := range cases {
		if got := detectDelimiter(content); got != want {
			t.Errorf("detectDelimiter(%q) = %q, want %q", content, string(got), string(want))
		}
	}
}
