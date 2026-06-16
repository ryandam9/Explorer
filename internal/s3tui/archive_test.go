package s3tui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
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
