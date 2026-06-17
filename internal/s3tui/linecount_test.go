package s3tui

import (
	"strings"
	"testing"
)

func TestCountLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"empty", "", 0},
		{"one line no newline", "a,b,c", 1},
		{"one line trailing newline", "a,b,c\n", 1},
		{"three lines trailing newline", "h\n1\n2\n", 3},
		{"three lines no trailing newline", "h\n1\n2", 3},
		{"blank lines counted", "a\n\n\nb\n", 4},
	}
	for _, c := range cases {
		got, err := countLines(strings.NewReader(c.in))
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: countLines(%q) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}

// TestCountLinesChunkBoundary feeds more than one read buffer's worth of data to
// make sure newlines straddling chunk boundaries are still counted.
func TestCountLinesChunkBoundary(t *testing.T) {
	const lines = 100_000
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString("row\n")
	}
	got, err := countLines(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("countLines: %v", err)
	}
	if got != lines {
		t.Errorf("countLines = %d, want %d", got, lines)
	}
}
