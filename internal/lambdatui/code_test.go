package lambdatui

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// Regression: the download-confirmation and the code browser are layered over
// the (still-active) detail view, so their keys must not be swallowed by the
// detail handler — a single "y" must start the download.
func TestCodeConfirmKeyNotSwallowedByDetail(t *testing.T) {
	mm := &m{
		detailActive: true,
		codeConfirm:  true,
		detailFunc:   FunctionDetail{Name: "fn", Region: "us-east-1", CodeLocation: "https://example/pkg.zip", PackageType: "Zip"},
	}
	mm.handleKey(keyMsg("y"))
	if mm.codeConfirm {
		t.Error("a single y should dismiss the confirmation")
	}
	if !mm.codeActive || !mm.codeLoading {
		t.Errorf("a single y should start the download: active=%v loading=%v", mm.codeActive, mm.codeLoading)
	}
}

func TestCodeConfirmEscCancels(t *testing.T) {
	mm := &m{detailActive: true, codeConfirm: true}
	mm.handleKey(keyMsg("esc"))
	if mm.codeConfirm || mm.codeActive {
		t.Errorf("esc should cancel: confirm=%v active=%v", mm.codeConfirm, mm.codeActive)
	}
}

// Esc closes the browser (back to the detail view) rather than being eaten by it.
func TestCodeBrowserEscClosesOverDetail(t *testing.T) {
	mm := &m{detailActive: true, codeActive: true}
	mm.handleKey(keyMsg("esc"))
	if mm.codeActive {
		t.Error("esc should close the code browser")
	}
	if !mm.detailActive {
		t.Error("closing the browser should leave the detail view open")
	}
}

// makeZip builds an in-memory zip from name→content pairs for the unzip tests.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestUnzipCode(t *testing.T) {
	data := makeZip(t, map[string]string{
		"handler.py":  "def handler(event, ctx):\n    return 'ok'\n",
		"bin/blob":    "text\x00with-nul", // a NUL marks it binary
		"lib/util.js": "module.exports = {}\n",
	})
	files, err := unzipCode(data)
	if err != nil {
		t.Fatalf("unzipCode: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	// Sorted by name: bin/blob, handler.py, lib/util.js.
	if files[0].Name != "bin/blob" || files[1].Name != "handler.py" {
		t.Errorf("not sorted: %q, %q", files[0].Name, files[1].Name)
	}
	if !files[0].Binary {
		t.Errorf("bin/blob should be flagged binary")
	}
	if files[1].Binary {
		t.Errorf("handler.py should not be binary")
	}
	if !strings.Contains(string(files[1].Data), "def handler") {
		t.Errorf("handler.py content missing: %q", files[1].Data)
	}
}

func TestUnzipCodeInvalid(t *testing.T) {
	if _, err := unzipCode([]byte("not a zip")); err == nil {
		t.Error("expected an error for non-zip data")
	}
}

func TestUnzipCodeTruncatesLargeFile(t *testing.T) {
	big := strings.Repeat("a", maxCodeFileBytes+100)
	files, err := unzipCode(makeZip(t, map[string]string{"big.txt": big}))
	if err != nil {
		t.Fatalf("unzipCode: %v", err)
	}
	if len(files) != 1 || !files[0].Truncated {
		t.Fatalf("expected one truncated file, got %+v", files)
	}
	if len(files[0].Data) != maxCodeFileBytes {
		t.Errorf("data len = %d, want %d", len(files[0].Data), maxCodeFileBytes)
	}
	if files[0].Size != int64(len(big)) {
		t.Errorf("declared size = %d, want %d", files[0].Size, len(big))
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte{'a', 0, 'b'}) {
		t.Error("NUL byte should be binary")
	}
	if isBinary([]byte("plain text")) {
		t.Error("plain text should not be binary")
	}
	if !isBinary([]byte{0xff, 0xfe, 0xfd}) {
		t.Error("invalid UTF-8 should be binary")
	}
}

func TestCodeFileContent(t *testing.T) {
	if got := codeFileContent(codeFile{Binary: true, Size: 2048}); !strings.Contains(got, "binary file") {
		t.Errorf("binary placeholder = %q", got)
	}
	if got := codeFileContent(codeFile{}); !strings.Contains(got, "empty file") {
		t.Errorf("empty placeholder = %q", got)
	}
	// Control bytes are stripped; a truncation note is appended.
	got := codeFileContent(codeFile{Data: []byte("a\x1b[31mb"), Truncated: true, Size: 9_999_999})
	if strings.Contains(got, "\x1b") {
		t.Errorf("escape not stripped: %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("missing truncation note: %q", got)
	}
}

// codeDisplay syntax-highlights source (losslessly — the text survives stripping
// the colour) and shows a placeholder for binary entries.
func TestCodeDisplay(t *testing.T) {
	mm := &m{}
	src := "def f():\n    return 1\n"
	got := ansi.Strip(mm.codeDisplay(codeFile{Name: "app.py", Data: []byte(src)}))
	if got != src {
		t.Errorf("highlighted source not lossless:\n got %q\nwant %q", got, src)
	}
	bin := mm.codeDisplay(codeFile{Name: "x.bin", Data: []byte{0, 1, 2}, Binary: true, Size: 3})
	if !strings.Contains(bin, "binary file") {
		t.Errorf("binary file = %q", bin)
	}
}

func TestCodeLangLabel(t *testing.T) {
	cases := map[string]string{"app.py": "py", "index.JS": "js", "Makefile": "—", "x.": "—"}
	for name, want := range cases {
		if got := codeLangLabel(name); got != want {
			t.Errorf("codeLangLabel(%q) = %q, want %q", name, got, want)
		}
	}
}
