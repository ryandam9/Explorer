package s3tui

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// PDF preview.
//
// What this does and does not do, since the difference matters: a PDF is laid
// out for a page, not a terminal, so there is no honest way to *show* one in a
// text UI. Rendering a page as an image would need a rasterizer — MuPDF or
// Poppler, both cgo or an external binary — which would end the pure-Go cross
// compile to six targets, Windows on ARM among them, for a preview.
//
// What is useful without any of that is the text: how many pages, what the
// document says it is, and the words on each page, which is what you are
// usually after when a PDF turns up in a bucket. It reads like any other text
// preview — same viewport, same grep. To actually look at the document, press
// D and open it in a reader.

// pdfTextCap bounds the extracted text. A long report would otherwise be
// rebuilt in memory in full, and nobody reads a thousand pages in a preview
// pane.
const pdfTextCap = 2 << 20

// pdfPageCap bounds how many pages are read. Extraction is the slow part, and
// the pane is not where a long document gets read.
const pdfPageCap = 200

// errPDFTruncated explains why a large PDF cannot be previewed. Like a zip, a
// PDF is read from a table at the *end* of the file (the xref), so a prefix of
// one is not a partial document but an unreadable one.
var errPDFTruncated = errors.New(
	"PDF is larger than the preview window — press D to download it and open it in a reader")

// errPDFMemberTooLarge explains why a PDF inside an archive cannot be
// previewed. Same reason as errPDFTruncated — the xref lives at the end of the
// file — but a different lever: the archive is already in memory, it is the
// per-member extraction window that was hit, so the archive is what to
// download.
var errPDFMemberTooLarge = fmt.Errorf(
	"PDF is larger than the %d MB per-file window — press Esc, then D to download the archive and open it in a reader",
	memberPreviewCap>>20)

// looksLikePDF reports whether a key names a PDF.
func looksLikePDF(key string) bool {
	return strings.HasSuffix(strings.ToLower(key), ".pdf")
}

// pdfMagicWindow bounds the content sniff. The PDF spec puts "%PDF-" at the
// start of the file, but readers tolerate leading junk, so look a little way
// in rather than only at offset 0.
const pdfMagicWindow = 1024

// looksLikePDFContent reports whether raw bytes are a PDF, for the cases where
// the name cannot say so: an archive member called "invoice" or "scan.bin"
// would otherwise hit the NUL-byte check and be written off as unreadable
// binary when its text is right there.
func looksLikePDFContent(data []byte) bool {
	return bytes.Contains(data[:min(len(data), pdfMagicWindow)], []byte("%PDF-"))
}

// Layout reconstruction.
//
// The library's GetPlainText walks the content stream and concatenates every
// string it draws, ignoring every operator that *moves* the cursor — Td, TD,
// Tm. But a PDF is not obliged to contain a single space character: a
// generator is free to place each word, or each line, at an absolute position
// and draw it on its own. Such a document came back as one unbroken run
// ("QuarterlycostreportEverywordhere…"), which is what the preview was
// showing.
//
// Page.Content gives every glyph with the position and advance the content
// stream actually computed, so the layout can be put back from the geometry:
// a different baseline is a different line, a horizontal gap wider than a
// fraction of the font size is a space, a much wider one is a column, and a
// vertical gap well over the page's usual leading is a paragraph break.
//
// Where a font carries no width metrics every advance reads as zero and all
// the glyphs of one string share an X. Gaps are then always zero, so no space
// is invented and the document's own spaces carry the words — while the line
// breaks, which come from Y alone, are still recovered. Under-space rather
// than mis-space: a missing gap is a smaller lie than one in the middle of a
// word.

const (
	// A space is about a quarter of an em in most fonts and inter-letter
	// gaps are near zero, so a fifth of the font size sits between them.
	pdfSpaceGapEm = 0.2
	// A gap this wide is a table column, not a word break; two spaces keep
	// the cells apart without pretending to align them.
	pdfColumnGapEm = 1.5
	// Baselines within this fraction of the font size are one line, which
	// absorbs the small shifts of an inline font change.
	pdfLineEpsilonEm = 0.25
	// A vertical gap this much larger than the page's usual leading is a
	// paragraph break rather than the next line.
	pdfParagraphFactor = 1.5
	// Used when a glyph reports no font size, so the thresholds above still
	// mean something.
	pdfFallbackFontSize = 12.0
)

// pdfLine accumulates the glyphs sharing one baseline.
type pdfLine struct {
	y    float64         // the baseline, in points, increasing up the page
	size float64         // the largest font size on the line — its em measure
	end  float64         // the right edge of the last glyph written
	b    strings.Builder // the line's text so far
	// endKnown is false once a glyph arrives with no width metric: its
	// right edge is then only the position it *started* at, so every
	// following gap is overstated by the width of a word. Gaps are still
	// used — they are the only thing holding such a document's words apart
	// — but they can no longer be trusted to tell a column from a space.
	endKnown bool
	// pending is the widest separator owed before the next visible glyph:
	// 0 none, 1 a space, 2 a column gap. Holding it rather than writing it
	// is what keeps a document that has both real spaces *and* positional
	// gaps from coming back double-spaced.
	pending int
}

// pdfPageText returns one page's text with its layout reconstructed.
//
// Content walks the content stream and panics on a malformed one — the
// library's own text helpers recover for exactly that reason — so the panic is
// turned into an error here: one bad page must not take down the preview of
// the rest (CLAUDE.md §3).
func pdfPageText(p pdf.Page) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("unreadable content stream: %v", r)
		}
	}()
	return pdfLayoutText(p.Content().Text), nil
}

// pdfLayoutText rebuilds reading order from positioned glyphs.
func pdfLayoutText(glyphs []pdf.Text) string {
	lines := pdfGroupLines(glyphs)
	if len(lines) == 0 {
		return ""
	}
	// Top of the page first. A stable sort keeps the drawing order of two
	// runs that landed on the same baseline.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].y > lines[j].y })

	gap := pdfTypicalLeading(lines)
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteString("\n")
			if gap > 0 && lines[i-1].y-ln.y > gap*pdfParagraphFactor {
				b.WriteString("\n") // a paragraph break
			}
		}
		b.WriteString(strings.TrimRight(ln.b.String(), " "))
	}
	return b.String()
}

// pdfGroupLines buckets glyphs onto shared baselines, inserting the spaces the
// document left to positioning. Buckets are keyed by the rounded baseline so a
// generator that draws a table column at a time still lands its cells on the
// row they belong to, rather than producing one line per cell.
func pdfGroupLines(glyphs []pdf.Text) []*pdfLine {
	var lines []*pdfLine
	byY := map[int][]*pdfLine{}

	for _, g := range glyphs {
		s := pdfStripControl(g.S)
		if s == "" {
			continue
		}
		size := g.FontSize
		if size <= 0 {
			size = pdfFallbackFontSize
		}
		ln := pdfFindLine(byY, g.Y, size)
		if ln == nil {
			ln = &pdfLine{y: g.Y, size: size, end: g.X, endKnown: true}
			lines = append(lines, ln)
			key := int(math.Round(g.Y))
			byY[key] = append(byY[key], ln)
		}
		if size > ln.size {
			ln.size = size
		}

		// The gap between where the last glyph ended and where this one
		// starts is what the document used instead of a space. A glyph
		// drawn to the *left* of the cursor is out-of-order drawing, not a
		// gap, so it counts as one separator rather than a negative one.
		if ln.b.Len() > 0 || ln.pending > 0 {
			switch gap := g.X - ln.end; {
			case gap >= pdfColumnGapEm*size && ln.endKnown:
				ln.pending = max(ln.pending, 2)
			case gap >= pdfSpaceGapEm*size, gap < -pdfSpaceGapEm*size:
				ln.pending = max(ln.pending, 1)
			}
		}

		// A space the document *did* write is held as a pending separator
		// too, so a real space and a positional gap in the same place
		// collapse into one rather than stacking up.
		if text := strings.TrimFunc(s, unicode.IsSpace); text == "" {
			if ln.b.Len() > 0 {
				ln.pending = max(ln.pending, 1)
			}
		} else {
			if ln.b.Len() > 0 {
				ln.b.WriteString(strings.Repeat(" ", ln.pending))
			}
			ln.b.WriteString(s)
			ln.pending = 0
		}

		if g.W > 0 {
			if end := g.X + g.W; end > ln.end {
				ln.end = end
			}
		} else {
			// No width metric: all that is known is where this glyph
			// began, so the line's right edge becomes a lower bound.
			if g.X > ln.end {
				ln.end = g.X
			}
			ln.endKnown = false
		}
	}
	return lines
}

// pdfFindLine returns the line this glyph belongs to, or nil for a new one.
// Only the buckets within the epsilon are examined, so this stays O(1) per
// glyph however long the page is.
func pdfFindLine(byY map[int][]*pdfLine, y, size float64) *pdfLine {
	eps := max(pdfLineEpsilonEm*size, 1.0)
	key := int(math.Round(y))
	span := int(math.Ceil(eps))

	var best *pdfLine
	bestDist := math.Inf(1)
	for k := key - span; k <= key+span; k++ {
		for _, ln := range byY[k] {
			if d := math.Abs(ln.y - y); d <= eps && d < bestDist {
				best, bestDist = ln, d
			}
		}
	}
	return best
}

// pdfTypicalLeading is the median distance between consecutive baselines — the
// page's own line spacing, which is what a paragraph break has to be measured
// against. A fixed multiple of the font size would call every line of
// generously leaded text a new paragraph.
func pdfTypicalLeading(lines []*pdfLine) float64 {
	if len(lines) < 3 {
		return 0 // too little to tell leading from a paragraph gap
	}
	gaps := make([]float64, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		if d := lines[i-1].y - lines[i].y; d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Float64s(gaps)
	// The lower middle for an even count: leading is the common gap, so
	// erring low keeps a page of a few lines from averaging a paragraph
	// break into the threshold meant to detect it.
	return gaps[(len(gaps)-1)/2]
}

// pdfStripControl drops the C0/C1 control characters, including the newline
// the library synthesizes after every TJ array — the line breaks here come
// from the geometry, not from characters in the stream.
func pdfStripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// pdfPreviewContent turns raw PDF bytes into preview text, or the reason they
// could not be read. Every path that lands on a PDF — a .pdf object, a
// .pdf.gz, an archive member — goes through here so the rule that a truncated
// PDF is unreadable (not partial) lives in one place and is reported with the
// lever that applies to that path.
func pdfPreviewContent(data []byte, truncated bool, tooLarge error) (string, error) {
	if truncated {
		return "", tooLarge
	}
	return pdfText(data)
}

// pdfText extracts a readable preview from raw PDF bytes: a header naming the
// page count, then each page's text under a marker.
//
// A PDF whose text cannot be extracted — a scan, an unusual encoding, a
// protected file — produces a note saying so rather than an empty pane, because
// "no text" and "this tool could not read the text" are different answers and
// the reader cannot tell them apart from a blank screen.
func pdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a readable PDF: %w", err)
	}
	pages := r.NumPage()
	if pages <= 0 {
		return "", errors.New("PDF has no pages")
	}

	var b strings.Builder
	shown := min(pages, pdfPageCap)
	fmt.Fprintf(&b, "PDF · %s", plural(pages, "page", "pages"))
	if shown < pages {
		fmt.Fprintf(&b, " · showing the first %d", shown)
	}
	b.WriteString("\n\n")

	var extracted, failed int
	truncated := false
	for i := 1; i <= shown; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			failed++
			continue
		}
		text, perr := pdfPageText(p)
		if perr != nil || strings.TrimSpace(text) == "" {
			// Fall back to the library's flat walk. It loses the layout —
			// that is what the positional pass is for — but it decodes
			// some streams the positional pass cannot, and a page of
			// run-together words beats a page of nothing.
			if flat, ferr := p.GetPlainText(nil); ferr == nil && strings.TrimSpace(flat) != "" {
				text, perr = flat, nil
			}
		}
		if perr != nil {
			failed++
			fmt.Fprintf(&b, "── page %d ──\n(could not read this page: %v)\n\n", i, perr)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			fmt.Fprintf(&b, "── page %d ──\n(no text on this page — an image or a scan)\n\n", i)
			continue
		}
		extracted++
		fmt.Fprintf(&b, "── page %d ──\n%s\n\n", i, text)
		if b.Len() > pdfTextCap {
			truncated = true
			break
		}
	}

	out := strings.TrimRight(b.String(), "\n")
	switch {
	case truncated:
		out += "\n\n… preview truncated. Press D to download the whole document."
	case extracted == 0:
		out += "\n\nNo text could be extracted. This is usually a scanned document — " +
			"the pages are images. Press D to download and open it in a reader."
	case failed > 0:
		out += fmt.Sprintf("\n\n%s could not be read; the rest is above.", plural(failed, "page", "pages"))
	}
	return out, nil
}

// plural renders a count with the right noun ("1 page", "12 pages").
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
