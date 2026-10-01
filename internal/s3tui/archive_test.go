package s3tui

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func gzipBytes(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.Bytes()
}

func tarGz(files map[string]string) []byte {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	// Deterministic order for the dir entry then files.
	tw.WriteHeader(&tar.Header{Name: "logs/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(content)), Mode: 0o644})
		tw.Write([]byte(content))
	}
	tw.Close()
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(raw.Bytes())
	gw.Close()
	return gz.Bytes()
}

func TestLooksLikeGzipVsTar(t *testing.T) {
	gz := []string{"app.log.gz", "data.csv.gz", "ACCESS.GZ"}
	for _, k := range gz {
		if !looksLikeGzip(k) {
			t.Errorf("%q should be a plain gzip", k)
		}
		if looksLikeTar(k) {
			t.Errorf("%q should not be a tar", k)
		}
	}
	tars := []string{"bundle.tar", "logs.tar.gz", "release.tgz"}
	for _, k := range tars {
		if !looksLikeTar(k) {
			t.Errorf("%q should be a tar archive", k)
		}
		if looksLikeGzip(k) {
			t.Errorf("%q should not be a plain gzip", k)
		}
	}
	if looksLikeGzip("notes.txt") || looksLikeTar("notes.txt") {
		t.Error("plain text misclassified")
	}
}

func TestInnerName(t *testing.T) {
	cases := map[string]string{
		"a/b/access.csv.gz": "access.csv",
		"app.log.gz":        "app.log",
		"dir/data.gz":       "data",
		"plain.txt":         "plain.txt",
	}
	for in, want := range cases {
		if got := innerName(in); got != want {
			t.Errorf("innerName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGunzip(t *testing.T) {
	out, truncated, err := gunzip(gzipBytes("hello, gzip world"), gzDecompressedCap)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || string(out) != "hello, gzip world" {
		t.Errorf("out=%q truncated=%v", out, truncated)
	}

	// Cap smaller than the content → truncated.
	big := strings.Repeat("x", 1000)
	out, truncated, err = gunzip(gzipBytes(big), 100)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(out) < 100 {
		t.Errorf("expected truncation: len=%d truncated=%v", len(out), truncated)
	}
}

func TestGunzipTruncatedInput(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "2026-06-16 00:%02d:%02d request id=%d path=/api/v%d/resource status=200\n", i%60, (i*7)%60, i, i%9)
	}
	full := gzipBytes(sb.String())
	// Chop the compressed stream to simulate a partial preview fetch.
	partial := full[:len(full)/2]
	out, _, err := gunzip(partial, gzDecompressedCap)
	if err != nil {
		t.Fatalf("truncated gzip should not error: %v", err)
	}
	if !strings.Contains(string(out), "request id=") {
		t.Errorf("expected partial content, got %d bytes", len(out))
	}
}

func TestTarMembersAndContent(t *testing.T) {
	data := tarGz(map[string]string{
		"logs/app.log":    "app log contents",
		"logs/access.csv": "id,name\n1,alice",
	})
	raw, _, err := gunzip(data, tarDecompressedCap)
	if err != nil {
		t.Fatal(err)
	}
	members, err := tarMembers(raw)
	if err != nil {
		t.Fatal(err)
	}
	// One dir + two files.
	var files, dirs int
	for _, m := range members {
		if m.Dir {
			dirs++
		} else {
			files++
		}
	}
	if files != 2 || dirs != 1 {
		t.Fatalf("members = %+v (files=%d dirs=%d)", members, files, dirs)
	}

	content, truncated, err := tarMemberContent(raw, "logs/app.log", memberPreviewCap)
	if err != nil || truncated || string(content) != "app log contents" {
		t.Errorf("member content = %q truncated=%v err=%v", content, truncated, err)
	}

	if _, _, err := tarMemberContent(raw, "logs/missing", memberPreviewCap); err == nil {
		t.Error("expected error for missing member")
	}
}

func TestOpenPreviewRouting(t *testing.T) {
	cases := []struct {
		key                string
		archive, csv, text bool
	}{
		{"logs.tar.gz", true, false, false},
		{"bundle.zip", true, false, false},
		{"BUNDLE.ZIP", true, false, false},
		{"bundle.tar", true, false, false},
		{"release.tgz", true, false, false},
		{"data.csv.gz", false, true, false}, // gz of a csv → CSV view
		{"app.log.gz", false, false, true},  // gz of a log → text view
		{"report.csv", false, true, false},
		{"notes.txt", false, false, true},
	}
	for _, c := range cases {
		m := &Model{bucket: "b"}
		_ = m.openPreview(c.key) // returns a command; not executed (no client needed)
		if m.showArchive != c.archive || m.showCSV != c.csv || m.showPreview != c.text {
			t.Errorf("%s: archive=%v csv=%v text=%v, want %v/%v/%v",
				c.key, m.showArchive, m.showCSV, m.showPreview, c.archive, c.csv, c.text)
		}
	}
}

func TestDecompressedPreview(t *testing.T) {
	if got := decompressedPreview([]byte("hello"), false, false); got != "hello" {
		t.Errorf("plain = %q", got)
	}
	if got := decompressedPreview([]byte("hello"), true, false); !strings.Contains(got, "truncated") {
		t.Errorf("truncated note missing: %q", got)
	}
	// CSV content must NOT get the note appended (it would corrupt the last row).
	if got := decompressedPreview([]byte("a,b\n1,2"), true, true); strings.Contains(got, "truncated") {
		t.Errorf("CSV should not get a truncation note: %q", got)
	}
	if got := decompressedPreview([]byte{'a', 0, 'b'}, false, false); !strings.Contains(got, "Binary") {
		t.Errorf("binary not detected: %q", got)
	}
}

// zipBytes builds an in-memory zip with a folder entry and the given files.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	if _, err := zw.Create("logs/"); err != nil { // a directory entry
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic archive
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestLooksLikeZip(t *testing.T) {
	for key, want := range map[string]bool{
		"bundle.zip":       true,
		"BUNDLE.ZIP":       true,
		"a/b/c.zip":        true,
		"archive.zipper":   false, // not an extension
		"logs.tar.gz":      false,
		"notes.txt":        false,
		"weird.zip.tar.gz": false, // the outer format wins
	} {
		if got := looksLikeZip(key); got != want {
			t.Errorf("looksLikeZip(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestZipMembersAndContent(t *testing.T) {
	data := zipBytes(t, map[string]string{
		"logs/app.log":  "hello from the zip",
		"logs/data.csv": "a,b\n1,2\n",
	})

	members, err := zipMembers(data)
	if err != nil {
		t.Fatalf("zipMembers: %v", err)
	}
	var files, dirs int
	sizes := map[string]int64{}
	for _, m := range members {
		if m.Dir {
			dirs++
			continue
		}
		files++
		sizes[m.Name] = m.Size
	}
	if files != 2 || dirs != 1 {
		t.Errorf("got %d files and %d folders, want 2 and 1", files, dirs)
	}
	if sizes["logs/app.log"] != int64(len("hello from the zip")) {
		t.Errorf("uncompressed size = %d, want %d", sizes["logs/app.log"], len("hello from the zip"))
	}

	out, truncated, err := zipMemberContent(data, "logs/app.log", memberPreviewCap)
	if err != nil {
		t.Fatalf("zipMemberContent: %v", err)
	}
	if string(out) != "hello from the zip" || truncated {
		t.Errorf("content = %q (truncated=%v), want the whole member", out, truncated)
	}

	// A member longer than the cap is cut and says so, rather than being read
	// into memory whole. (readCapped keeps the byte it peeks to detect the
	// overrun, so the result is the cap plus one — the same contract the tar
	// path has always had.)
	short, truncated, err := zipMemberContent(data, "logs/app.log", 5)
	if err != nil {
		t.Fatalf("capped read: %v", err)
	}
	if !truncated {
		t.Error("a member longer than the cap must report truncation")
	}
	if len(short) > 6 || !strings.HasPrefix("hello from the zip", string(short)) {
		t.Errorf("capped content = %q, want a short prefix of the member", short)
	}

	// A name that isn't in the archive, and a folder entry, are both "not a
	// file you can open" rather than an empty preview.
	if _, _, err := zipMemberContent(data, "logs/missing.log", memberPreviewCap); err == nil {
		t.Error("expected an error for a member that is not in the archive")
	}
	if _, _, err := zipMemberContent(data, "logs/", memberPreviewCap); err == nil {
		t.Error("expected an error when opening a directory entry")
	}
}

// A zip is read from the central directory at the end of the file, so a
// prefix of one is unreadable rather than partial. Go's reader says "not a
// valid zip file", which is wrong and alarming for a file that is perfectly
// valid and merely large — the UI must say what actually happened.
func TestZipMembersRejectsATruncatedArchive(t *testing.T) {
	data := zipBytes(t, map[string]string{"logs/app.log": strings.Repeat("x", 4096)})
	if _, err := zipMembers(data[:len(data)/2]); err == nil {
		t.Fatal("a truncated zip should not parse")
	}
	if !strings.Contains(errZipTooLarge.Error(), "download it") {
		t.Errorf("the too-large message should tell the reader what to do instead: %q", errZipTooLarge)
	}
	if !strings.Contains(errZipTooLarge.Error(), "32 MB") {
		t.Errorf("the too-large message should name the limit: %q", errZipTooLarge)
	}
}

// The member reader follows the archive's own format, so a zip member is not
// read with the tar reader (which would simply not find it).
func TestArchiveMemberContentPicksTheFormat(t *testing.T) {
	zipData := zipBytes(t, map[string]string{"a.txt": "from zip"})
	out, _, err := archiveMemberContent("bundle.zip", zipData, "a.txt", memberPreviewCap)
	if err != nil || string(out) != "from zip" {
		t.Errorf("zip member = %q, err=%v; want \"from zip\"", out, err)
	}

	tarData := tarGzRaw(map[string]string{"a.txt": "from tar"})
	out, _, err = archiveMemberContent("bundle.tar", tarData, "a.txt", memberPreviewCap)
	if err != nil || string(out) != "from tar" {
		t.Errorf("tar member = %q, err=%v; want \"from tar\"", out, err)
	}
}

// tarGzRaw builds an uncompressed tar, which is what the archive browser holds
// in memory after gunzipping.
func tarGzRaw(files map[string]string) []byte {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(content)), Mode: 0o644})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	return raw.Bytes()
}
