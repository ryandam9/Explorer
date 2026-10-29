package s3tui

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// breakZipDirectoryOffset inflates the end-of-central-directory record's
// "offset of start of central directory" field.
//
// This is the real malformation, reproduced: Go's reader reads the gap between
// the recorded offset and the directory's true position as a prefix on the
// archive and subtracts it from every offset, so the directory is still found
// (the correction cancels out there) while every entry offset goes negative.
// Such an archive lists perfectly and then fails to open anything in it with
// "bytes.Reader.ReadAt: negative offset".
func breakZipDirectoryOffset(t *testing.T, data []byte) []byte {
	t.Helper()
	out := bytes.Clone(data)
	i := bytes.LastIndex(out, []byte("PK\x05\x06"))
	if i < 0 {
		t.Fatal("no end-of-central-directory record in the fixture")
	}
	const offsetField = 16 // bytes into the EOCD record
	off := binary.LittleEndian.Uint32(out[i+offsetField:])
	binary.LittleEndian.PutUint32(out[i+offsetField:], off+100_000)
	return out
}

// The fixture must actually reproduce the bug, or the tests below prove
// nothing: the archive lists, and the stock reader cannot open a member.
func TestBrokenZipFixtureReproducesTheFailure(t *testing.T) {
	data := breakZipDirectoryOffset(t, zipBytes(t, map[string]string{
		"docs/a.pdf": "%PDF-1.4 pretend",
		"notes.txt":  "hello from the zip",
	}))

	members, err := zipMembers(data)
	if err != nil {
		t.Fatalf("listing should still work: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("listing returned nothing")
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	var sawNegative bool
	for _, f := range zr.File {
		if _, oerr := f.Open(); oerr != nil && strings.Contains(oerr.Error(), "negative offset") {
			sawNegative = true
		}
	}
	if !sawNegative {
		t.Fatal("fixture does not reproduce the negative-offset failure")
	}
}

// The member is intact in the file; only the arithmetic pointing at it is
// wrong. Scanning for its local header finds it, and the read is flagged as
// repaired so the UI can say the archive is malformed.
func TestZipMemberContentRecoversFromBadOffsets(t *testing.T) {
	want := map[string]string{
		"docs/a.pdf": "%PDF-1.4 first document",
		"docs/b.pdf": "%PDF-1.4 second document",
		"notes.txt":  "hello from the zip",
	}
	data := breakZipDirectoryOffset(t, zipBytes(t, want))

	for name, body := range want {
		out, truncated, repaired, err := zipMemberContent(data, name, memberPreviewCap)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if string(out) != body {
			t.Errorf("%s = %q, want %q", name, out, body)
		}
		if truncated {
			t.Errorf("%s reported truncated", name)
		}
		if !repaired {
			t.Errorf("%s was recovered but not flagged as repaired", name)
		}
	}
}

// A healthy archive must not be flagged, and must not go anywhere near the
// fallback.
func TestZipMemberContentHealthyArchiveIsNotFlagged(t *testing.T) {
	data := zipBytes(t, map[string]string{"notes.txt": "hello from the zip"})

	out, _, repaired, err := zipMemberContent(data, "notes.txt", memberPreviewCap)
	if err != nil {
		t.Fatalf("zipMemberContent: %v", err)
	}
	if string(out) != "hello from the zip" {
		t.Errorf("content = %q", out)
	}
	if repaired {
		t.Error("a healthy archive was flagged as repaired")
	}
}

// The cap still applies on the recovered path — a repaired read must not
// become an unbounded one.
func TestZipMemberContentRepairedReadIsStillCapped(t *testing.T) {
	data := breakZipDirectoryOffset(t, zipBytes(t, map[string]string{
		"big.log": strings.Repeat("x", 4096),
	}))

	out, truncated, repaired, err := zipMemberContent(data, "big.log", 64)
	if err != nil {
		t.Fatalf("zipMemberContent: %v", err)
	}
	if !repaired {
		t.Error("expected the repaired path")
	}
	if !truncated {
		t.Error("a capped read must report truncation")
	}
	if len(out) > 128 { // readCapped keeps one peeked byte past the cap
		t.Errorf("read %d bytes for a cap of 64", len(out))
	}
}

// A member that is genuinely absent must still be reported as absent — the
// scan must not turn "missing" into some other file's bytes.
func TestZipMemberContentMissingMemberStillFails(t *testing.T) {
	data := breakZipDirectoryOffset(t, zipBytes(t, map[string]string{"notes.txt": "hello"}))

	if _, _, _, err := zipMemberContent(data, "nope.pdf", memberPreviewCap); err == nil {
		t.Fatal("expected an error for a member that is not in the archive")
	}
}

// When neither route works the error has to name both failures, so the reason
// is diagnosable rather than just "cannot read".
func TestZipRepairErrorNamesBothFailures(t *testing.T) {
	data := breakZipDirectoryOffset(t, zipBytes(t, map[string]string{"notes.txt": "hello"}))
	// Blank every local header signature so the scan cannot find one
	// either — the archive is then damaged beyond recovery.
	if !bytes.Contains(data, []byte("PK\x03\x04")) {
		t.Fatal("no local header in the fixture")
	}
	data = bytes.ReplaceAll(data, []byte("PK\x03\x04"), []byte("XXXX"))

	_, _, _, err := zipMemberContent(data, "notes.txt", memberPreviewCap)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"notes.txt", "negative offset", "scanning"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// The scan matches on the member's name, so a "PK\x03\x04" that happens to
// occur inside stored data is not mistaken for a header.
func TestZipMemberBodyIgnoresASignatureInsideContent(t *testing.T) {
	body := "before PK\x03\x04 after"
	data := breakZipDirectoryOffset(t, zipBytes(t, map[string]string{"notes.txt": body}))

	out, _, repaired, err := zipMemberContent(data, "notes.txt", memberPreviewCap)
	if err != nil {
		t.Fatalf("zipMemberContent: %v", err)
	}
	if !repaired {
		t.Error("expected the repaired path")
	}
	if string(out) != body {
		t.Errorf("content = %q, want %q", out, body)
	}
}

// End to end: the case that reported this — a zip of PDFs that lists fine and
// then fails to open one. The PDF's text comes out, and the panel says the
// archive is malformed.
func TestOpenArchiveMemberPDFFromBrokenZip(t *testing.T) {
	data := breakZipDirectoryOffset(t, zipWithPDF(t, "docs/report.pdf"))
	m := openZipMember(t, data, "docs/report.pdf")

	if m.previewErr != nil {
		t.Fatalf("preview errored: %v", m.previewErr)
	}
	if !strings.Contains(m.previewContent, "Hello from page one") {
		t.Errorf("PDF text not extracted:\n%s", m.previewContent)
	}
	if !m.previewRepaired {
		t.Error("the reader was not told the archive index is wrong")
	}
	if frame := m.previewView(); !strings.Contains(frame, "archive index is wrong") {
		t.Errorf("panel does not carry the malformed-archive note:\n%s", frame)
	}
}
