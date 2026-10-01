package s3tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

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
		text, perr := p.GetPlainText(nil)
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
