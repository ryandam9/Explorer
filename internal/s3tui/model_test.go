package s3tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/ui"
)

func TestFormatAndParseSize(t *testing.T) {
	tests := []struct {
		bytes     int64
		formatted string
		parsedMin int64
	}{
		{0, "0 B", 0},
		{12, "12 B", 12},
		{1536, "1.5 KB", 1536},
		{2 * 1024 * 1024, "2.0 MB", 2 * 1024 * 1024},
		{3 * 1024 * 1024 * 1024, "3.0 GB", 3 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		if got := formatSize(tt.bytes); got != tt.formatted {
			t.Fatalf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.formatted)
		}
		if got := parseSize(tt.formatted); got != tt.parsedMin {
			t.Fatalf("parseSize(%q) = %d, want %d", tt.formatted, got, tt.parsedMin)
		}
	}
}

func TestSortObjectsKeepsDirectoriesFirstAndSortsSize(t *testing.T) {
	m := &Model{sortCol: 1, sortAsc: true}
	objs := []map[string]string{
		{"name": "z.txt",    "type": "FILE", "size": "2.0 MB", "last_modified": "2026-01-01", "storage_class": "STANDARD", "etag": "etag-z"},
		{"name": "photos/",  "type": "DIR",  "size": "-",      "last_modified": "-",          "storage_class": "DIR",      "etag": "-"},
		{"name": "a.txt",    "type": "FILE", "size": "10 B",   "last_modified": "2026-01-01", "storage_class": "STANDARD", "etag": "etag-a"},
		{"name": "b.txt",    "type": "FILE", "size": "1.5 KB", "last_modified": "2026-01-01", "storage_class": "STANDARD", "etag": "etag-b"},
	}

	m.sortObjects(objs)

	wantNames := []string{"photos/", "a.txt", "b.txt", "z.txt"}
	for i, want := range wantNames {
		if got := objs[i]["name"]; got != want {
			t.Fatalf("row %d name = %q, want %q; objs=%v", i, got, want, objs)
		}
	}
}

func TestSortObjectsNameDescendingCaseInsensitive(t *testing.T) {
	m := &Model{sortCol: 0, sortAsc: false}
	objs := []map[string]string{
		{"name": "alpha.txt",   "type": "FILE", "size": "1 B", "storage_class": "STANDARD"},
		{"name": "Bravo.txt",   "type": "FILE", "size": "1 B", "storage_class": "STANDARD"},
		{"name": "charlie.txt", "type": "FILE", "size": "1 B", "storage_class": "STANDARD"},
	}

	m.sortObjects(objs)

	wantNames := []string{"charlie.txt", "Bravo.txt", "alpha.txt"}
	for i, want := range wantNames {
		if got := objs[i]["name"]; got != want {
			t.Fatalf("row %d name = %q, want %q", i, got, want)
		}
	}
}

func TestFeatherRailRendersEveryThemeColor(t *testing.T) {
	ui.SetActiveTheme(0)
	const width = 10
	// FeatherRail should render exactly `width` characters, cycling through theme colors.
	if got := lipgloss.Width(ui.FeatherRail(width)); got != width {
		t.Fatalf("FeatherRail width = %d, want %d", got, width)
	}
}

func TestResolveDownloadDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}

	cases := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"nil config", nil, "."},
		{"empty value", &config.Config{}, "."},
		{"whitespace value", &config.Config{App: config.AppConfig{DownloadDir: "   "}}, "."},
		{"explicit dir", &config.Config{App: config.AppConfig{DownloadDir: "/tmp/dl"}}, "/tmp/dl"},
		{"tilde alone", &config.Config{App: config.AppConfig{DownloadDir: "~"}}, home},
		{"tilde prefix", &config.Config{App: config.AppConfig{DownloadDir: "~/Downloads"}}, filepath.Join(home, "Downloads")},
	}

	for _, tc := range cases {
		if got := resolveDownloadDir(tc.cfg); got != tc.want {
			t.Fatalf("%s: resolveDownloadDir = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParentPrefix(t *testing.T) {
	tests := map[string]string{
		"":                    "",
		"logs/":               "",
		"logs/2026/":          "logs/",
		"logs/2026/06/":       "logs/2026/",
		"logs/2026/06":        "logs/2026/",
		"one/two/three/four/": "one/two/three/",
	}

	for input, want := range tests {
		if got := parentPrefix(input); got != want {
			t.Fatalf("parentPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()

	p := uniquePath(dir, "data.csv")
	if p != filepath.Join(dir, "data.csv") {
		t.Fatalf("first download should use the plain name, got %q", p)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p = uniquePath(dir, "data.csv")
	if p != filepath.Join(dir, "data (1).csv") {
		t.Fatalf("existing file should yield a (1) suffix, got %q", p)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if p = uniquePath(dir, "data.csv"); p != filepath.Join(dir, "data (2).csv") {
		t.Fatalf("second collision should yield a (2) suffix, got %q", p)
	}

	// Extension-less names get the suffix at the end.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p = uniquePath(dir, "README"); p != filepath.Join(dir, "README (1)") {
		t.Fatalf("extension-less collision suffix wrong, got %q", p)
	}
}
