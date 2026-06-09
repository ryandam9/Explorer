package vpctui

import (
	"strings"
	"testing"
)

func boolp(b bool) *bool { return &b }

func TestAnalysisVerdict(t *testing.T) {
	cases := []struct {
		a    NetInsightsAnalysis
		want string
	}{
		{NetInsightsAnalysis{Status: "running"}, "running"},
		{NetInsightsAnalysis{Status: "failed"}, "failed"},
		{NetInsightsAnalysis{Status: "succeeded", PathFound: boolp(true)}, "reachable"},
		{NetInsightsAnalysis{Status: "succeeded", PathFound: boolp(false)}, "not reachable"},
		{NetInsightsAnalysis{Status: "succeeded"}, "not reachable"}, // nil PathFound
	}
	for _, c := range cases {
		if got := analysisVerdict(c.a); got != c.want {
			t.Errorf("analysisVerdict(%+v) = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestAnalysisGlyph(t *testing.T) {
	want := map[string]string{
		"reachable":     "✓",
		"not reachable": "✗",
		"failed":        "⚠",
		"running":       "…",
	}
	cases := map[string]NetInsightsAnalysis{
		"reachable":     {Status: "succeeded", PathFound: boolp(true)},
		"not reachable": {Status: "succeeded", PathFound: boolp(false)},
		"failed":        {Status: "failed"},
		"running":       {Status: "running"},
	}
	for verdict, a := range cases {
		if got := analysisGlyph(a); got != want[verdict] {
			t.Errorf("glyph for %s = %q, want %q", verdict, got, want[verdict])
		}
	}
}

func TestAnalysisLine(t *testing.T) {
	a := NetInsightsAnalysis{
		Source: "eni-a", Destination: "eni-b", Protocol: "tcp", DestPort: 443,
		Status: "succeeded", PathFound: boolp(true),
	}
	line := analysisLine(a)
	for _, want := range []string{"✓", "eni-a", "eni-b:443", "(tcp)", "[reachable]"} {
		if !strings.Contains(line, want) {
			t.Errorf("analysisLine = %q, missing %q", line, want)
		}
	}
}

func TestParseAnalyzerInput(t *testing.T) {
	cases := []struct {
		in     string
		source string
		dest   string
		port   int
		ok     bool
	}{
		{"eni-a -> eni-b:443", "eni-a", "eni-b", 443, true},
		{"eni-a -> igw-1", "eni-a", "igw-1", -1, true},
		{"i-123->i-456:22", "i-123", "i-456", 22, true},
		{"eni-a", "", "", 0, false},        // no separator
		{"-> eni-b:443", "", "", 0, false}, // missing source
		{"eni-a ->", "", "", 0, false},     // missing dest
	}
	for _, c := range cases {
		src, dst, port, ok := parseAnalyzerInput(c.in)
		if ok != c.ok || (ok && (src != c.source || dst != c.dest || port != c.port)) {
			t.Errorf("parseAnalyzerInput(%q) = (%q,%q,%d,%v), want (%q,%q,%d,%v)",
				c.in, src, dst, port, ok, c.source, c.dest, c.port, c.ok)
		}
	}
}
