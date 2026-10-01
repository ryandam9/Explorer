package s3tui

import (
	"os"
	"strings"
	"testing"
)

func TestLooksLikePDF(t *testing.T) {
	for key, want := range map[string]bool{
		"report.pdf":      true,
		"REPORT.PDF":      true,
		"a/b/invoice.pdf": true,
		"report.pdf.gz":   false, // the outer format wins
		"notes.txt":       false,
		"pdf":             false,
	} {
		if got := looksLikePDF(key); got != want {
			t.Errorf("looksLikePDF(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestPDFText(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Fatal(err)
	}
	out, err := pdfText(data)
	if err != nil {
		t.Fatalf("pdfText: %v", err)
	}
	for _, want := range []string{
		"PDF · 2 pages",       // the header says how much there is
		"── page 1 ──",        // pages are separated and numbered…
		"Hello from page one", // …and carry their text
		"── page 2 ──",
		"And here is page two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
}

// A file that is not a PDF fails with a readable reason, not a panic or an
// empty pane.
func TestPDFTextRejectsNonPDF(t *testing.T) {
	if _, err := pdfText([]byte("this is just text")); err == nil {
		t.Fatal("expected an error for a non-PDF")
	} else if !strings.Contains(err.Error(), "not a readable PDF") {
		t.Errorf("error should say what is wrong, got %q", err)
	}

	// Truncated at the front half: the xref at the end is gone, so it cannot
	// be read at all. This is why the fetch refuses rather than parsing.
	data, rerr := os.ReadFile("testdata/sample.pdf")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if _, err := pdfText(data[:len(data)/2]); err == nil {
		t.Error("a truncated PDF should not parse")
	}
	if !strings.Contains(errPDFTruncated.Error(), "download") {
		t.Errorf("the too-large message should say what to do instead: %q", errPDFTruncated)
	}
}

// A PDF whose pages carry no extractable text — a scan — says so. An empty
// pane cannot be told apart from "this tool failed".
func TestPDFTextSaysWhenThereIsNoText(t *testing.T) {
	data, err := os.ReadFile("testdata/blank.pdf")
	if err != nil {
		t.Skipf("no blank fixture: %v", err)
	}
	out, err := pdfText(data)
	if err != nil {
		t.Fatalf("pdfText: %v", err)
	}
	if !strings.Contains(out, "No text could be extracted") && !strings.Contains(out, "no text on this page") {
		t.Errorf("a textless PDF should say so:\n%s", out)
	}
}
