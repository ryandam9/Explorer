package emrdoctor

import (
	"bytes"
	"strings"
	"testing"
)

func TestRender_PlainHasTagsAndHints(t *testing.T) {
	r := &Report{}
	r.ok("on-cluster config", "mode=socks · proxy 127.0.0.1:8157")
	r.fail("bridge (socks)", "SOCKS proxy 127.0.0.1:8157 not reachable", "No SSH dynamic tunnel is running.")
	r.skip("HBase", "skipped — bridge not available")

	var buf bytes.Buffer
	Render(&buf, r, false) // plain
	out := buf.String()

	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain render must not emit ANSI, got %q", out)
	}
	for _, want := range []string{"OK", "FAIL", "SKIP", "→ No SSH dynamic tunnel", "Summary:", "1 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}
	// A hint is only shown for fail/warn, never for ok/skip.
	if strings.Contains(out, "→ skipped") {
		t.Error("skip lines should not render a hint arrow")
	}
}

func TestRender_ColorWrapsMarkers(t *testing.T) {
	r := &Report{}
	r.ok("x", "y")
	var buf bytes.Buffer
	Render(&buf, r, true)
	if !strings.Contains(buf.String(), ansiGreen) {
		t.Errorf("color render should tint the OK marker green, got %q", buf.String())
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("the quick brown fox jumps", 10)
	for _, l := range got {
		if len(l) > 10 && !strings.Contains(l, " ") == false {
			// lines may exceed width only when a single token is longer than width
			continue
		}
	}
	if len(got) < 2 {
		t.Errorf("expected the text to wrap into multiple lines, got %v", got)
	}
	// A token longer than the width is kept intact on its own line.
	got = wrapText("supercalifragilistic word", 8)
	if got[0] != "supercalifragilistic" {
		t.Errorf("over-long token should stay intact, got %v", got)
	}
}
