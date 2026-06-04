package s3tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/tui"
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
	rows := []table.Row{
		{"z.txt", "2.0 MB", "2026-01-01", "STANDARD", "etag-z"},
		{"photos/", "-", "-", "DIR", "-"},
		{"a.txt", "10 B", "2026-01-01", "STANDARD", "etag-a"},
		{"b.txt", "1.5 KB", "2026-01-01", "STANDARD", "etag-b"},
	}

	m.sortObjects(rows)

	wantNames := []string{"photos/", "a.txt", "b.txt", "z.txt"}
	for i, want := range wantNames {
		if got := rows[i][0]; got != want {
			t.Fatalf("row %d name = %q, want %q; rows=%v", i, got, want, rows)
		}
	}
}

func TestSortObjectsNameDescendingCaseInsensitive(t *testing.T) {
	m := &Model{sortCol: 0, sortAsc: false}
	rows := []table.Row{
		{"alpha.txt", "1 B", "", "STANDARD", ""},
		{"Bravo.txt", "1 B", "", "STANDARD", ""},
		{"charlie.txt", "1 B", "", "STANDARD", ""},
	}

	m.sortObjects(rows)

	wantNames := []string{"charlie.txt", "Bravo.txt", "alpha.txt"}
	for i, want := range wantNames {
		if got := rows[i][0]; got != want {
			t.Fatalf("row %d name = %q, want %q", i, got, want)
		}
	}
}

func TestFeatherRailRendersEveryThemeColor(t *testing.T) {
	tui.SetActiveTheme(0)
	const width = 10
	// FeatherRail should render exactly `width` characters, cycling through theme colors.
	if got := lipgloss.Width(tui.FeatherRail(width)); got != width {
		t.Fatalf("FeatherRail width = %d, want %d", got, width)
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
