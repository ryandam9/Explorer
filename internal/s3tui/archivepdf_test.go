package s3tui

import (
	"os"
	"strings"
	"testing"
)

// zipWithPDF builds a zip holding testdata/sample.pdf under the given member
// name, plus a plain text file so the member table has more than one row.
func zipWithPDF(t *testing.T, member string) []byte {
	t.Helper()
	pdfBytes, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Fatal(err)
	}
	return zipBytes(t, map[string]string{
		"notes.txt": "just text",
		member:      string(pdfBytes),
	})
}

// openZipMember loads a zip into a Model and opens the named member, returning
// the model so the preview state can be asserted on.
func openZipMember(t *testing.T, data []byte, member string) *Model {
	t.Helper()
	m := &Model{width: 100, height: 30, bucket: "b"}
	members, err := zipMembers(data)
	if err != nil {
		t.Fatalf("zipMembers: %v", err)
	}
	m.archiveKey = "bundle.zip"
	m.applyArchiveLoaded(archiveLoadedMsg{key: m.archiveKey, data: data, members: members})

	files := m.fileMembers()
	idx := -1
	for i, f := range files {
		if f.Name == member {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("member %q not in archive", member)
	}
	m.archiveTable.SetCursor(idx)
	m.openArchiveMember()
	return m
}

// A PDF inside a zip is text-extracted like a top-level .pdf, not written off
// as binary by the NUL-byte check.
func TestOpenArchiveMemberPDF(t *testing.T) {
	m := openZipMember(t, zipWithPDF(t, "docs/report.pdf"), "docs/report.pdf")

	if m.previewErr != nil {
		t.Fatalf("preview errored: %v", m.previewErr)
	}
	if !m.showPreview || m.showCSV || m.showArchive {
		t.Errorf("want the text preview open, got showPreview=%v showCSV=%v showArchive=%v",
			m.showPreview, m.showCSV, m.showArchive)
	}
	for _, want := range []string{"PDF · 2 pages", "Hello from page one", "And here is page two"} {
		if !strings.Contains(m.previewContent, want) {
			t.Errorf("preview missing %q:\n%s", want, m.previewContent)
		}
	}
	if strings.Contains(m.previewContent, "Binary content") {
		t.Errorf("PDF member reported as binary:\n%s", m.previewContent)
	}
}

// The name need not say "pdf": a member called "invoice" is sniffed from its
// bytes, because the alternative is telling the user there is nothing to see.
func TestOpenArchiveMemberPDFByContent(t *testing.T) {
	m := openZipMember(t, zipWithPDF(t, "invoice"), "invoice")

	if m.previewErr != nil {
		t.Fatalf("preview errored: %v", m.previewErr)
	}
	if !strings.Contains(m.previewContent, "Hello from page one") {
		t.Errorf("extensionless PDF member not extracted:\n%s", m.previewContent)
	}
}

// A non-PDF member still takes the ordinary path — the sniff must not capture
// everything binary.
func TestOpenArchiveMemberBinaryStillBinary(t *testing.T) {
	data := zipBytes(t, map[string]string{"blob.bin": "head\x00tail"})
	m := openZipMember(t, data, "blob.bin")

	if !strings.Contains(m.previewContent, "Binary content") {
		t.Errorf("non-PDF binary should still be flagged, got:\n%s", m.previewContent)
	}
}
