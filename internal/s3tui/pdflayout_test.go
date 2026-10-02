package s3tui

import (
	"os"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

// glyphs turns a compact spec into positioned glyphs: one Text per rune,
// advancing x by w each time, so a test can describe a line the way the
// content stream draws it.
func glyphs(s string, x, y, size, w float64) []pdf.Text {
	out := make([]pdf.Text, 0, len(s))
	for _, r := range s {
		out = append(out, pdf.Text{FontSize: size, X: x, Y: y, W: w, S: string(r)})
		x += w
	}
	return out
}

// testdata/layout.pdf places every word at an absolute position and contains
// no space character at all — the shape of document the library's flat walk
// returns as one unbroken run.
func TestPDFTextRecoversLayout(t *testing.T) {
	data, err := os.ReadFile("testdata/layout.pdf")
	if err != nil {
		t.Fatal(err)
	}
	out, err := pdfText(data)
	if err != nil {
		t.Fatalf("pdfText: %v", err)
	}

	// The bug: every word glued to the next.
	if strings.Contains(out, "Quarterlycostreport") {
		t.Errorf("words still concatenated:\n%s", out)
	}
	for _, want := range []string{
		"Quarterly cost report",
		"Every word here is placed on its own.",
		"There is not one space character.",
		"A second paragraph starts lower down.",
		"us-east-1  $12.34", // a wide gap reads as a column, not a word break
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The 42pt drop to the second paragraph is well over the page's 14pt
	// leading, so it reads as a break.
	if !strings.Contains(out, "character.\n\nA second paragraph") {
		t.Errorf("paragraph break not recovered:\n%s", out)
	}
}

// A document that has real space characters *and* positional gaps must not
// come back double-spaced.
func TestPDFLayoutDoesNotDoubleSpace(t *testing.T) {
	var g []pdf.Text
	g = append(g, glyphs("one", 0, 100, 10, 6)...)
	g = append(g, pdf.Text{FontSize: 10, X: 18, Y: 100, W: 3, S: " "}) // a real space…
	g = append(g, glyphs("two", 24, 100, 10, 6)...)                    // …plus a gap

	if got := pdfLayoutText(g); got != "one two" {
		t.Errorf("got %q, want %q", got, "one two")
	}
}

// Where the font carries no widths every advance is zero, so a gap measured
// from a glyph's start overstates the real one. Lines must still split, and
// the overstated gap must not be promoted to a column.
func TestPDFLayoutWithoutWidths(t *testing.T) {
	var g []pdf.Text
	// Two runs on one baseline, drawn 40pt apart with zero-width glyphs.
	g = append(g, glyphs("Region", 0, 100, 10, 0)...)
	g = append(g, glyphs("Cost", 40, 100, 10, 0)...)
	g = append(g, glyphs("second line", 0, 86, 10, 0)...)

	want := "Region Cost\nsecond line"
	if got := pdfLayoutText(g); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A generator that draws a table one column at a time still has to come back
// row by row — the baseline, not the drawing order, says what a line is.
func TestPDFLayoutGroupsByBaselineNotDrawOrder(t *testing.T) {
	var g []pdf.Text
	g = append(g, glyphs("a1", 0, 100, 10, 6)...)  // column 1, both rows…
	g = append(g, glyphs("a2", 0, 88, 10, 6)...)   //
	g = append(g, glyphs("b1", 60, 100, 10, 6)...) // …then column 2
	g = append(g, glyphs("b2", 60, 88, 10, 6)...)

	want := "a1  b1\na2  b2"
	if got := pdfLayoutText(g); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A blank line marks a paragraph, so the threshold has to come from the page's
// own leading — a fixed multiple of the font size would break every line of
// generously leaded text into its own paragraph.
func TestPDFLayoutParagraphUsesPageLeading(t *testing.T) {
	var g []pdf.Text
	for i, y := range []float64{100, 80, 60, 40} { // 20pt leading at 10pt type
		g = append(g, glyphs(string(rune('a'+i)), 0, y, 10, 6)...)
	}
	if got := pdfLayoutText(g); got != "a\nb\nc\nd" {
		t.Errorf("leaded lines split into paragraphs: %q", got)
	}

	g = append(g, glyphs("e", 0, -10, 10, 6)...) // a 50pt drop
	if got := pdfLayoutText(g); !strings.HasSuffix(got, "d\n\ne") {
		t.Errorf("real paragraph gap not detected: %q", got)
	}
}

// The newline the library synthesizes after every TJ array is a character in
// the glyph stream, not a line break — the geometry decides lines here.
func TestPDFLayoutDropsControlGlyphs(t *testing.T) {
	g := []pdf.Text{
		{FontSize: 10, X: 0, Y: 100, W: 6, S: "a"},
		{FontSize: 10, X: 6, Y: 100, W: 0, S: "\n"},
		{FontSize: 10, X: 6, Y: 100, W: 6, S: "b"},
	}
	if got := pdfLayoutText(g); got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}
}

// Nothing to lay out is not an error — the caller turns it into the "no text
// on this page" note, which is a different answer from a failure.
func TestPDFLayoutEmpty(t *testing.T) {
	if got := pdfLayoutText(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
