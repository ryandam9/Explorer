package clilog

import (
	"bytes"
	"strings"
	"testing"
)

func TestColorizeLineByLevel(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantCode string // an ANSI code expected somewhere in the output
		whole    bool   // the colour should wrap the whole line (warn/error)
	}{
		{"info tokens only", `time=t level=INFO msg="hi"`, green, false},
		{"warn whole line", `time=t level=WARN msg="nope"`, yellow, true},
		{"error whole line", `time=t level=ERROR msg="boom"`, red, true},
		{"debug tokens only", `time=t level=DEBUG msg="trace"`, gray, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorizeLine(tt.line)
			if !strings.Contains(got, tt.wantCode) {
				t.Fatalf("colorizeLine(%q) = %q, missing code %q", tt.line, got, tt.wantCode)
			}
			if !strings.Contains(got, reset) {
				t.Errorf("colorizeLine(%q) should contain a reset, got %q", tt.line, got)
			}
			// Whole-line colouring ends with a reset; token colouring resets
			// mid-line and leaves the message uncoloured after it.
			if tt.whole && !strings.HasSuffix(got, reset) {
				t.Errorf("colorizeLine(%q) should end with a reset, got %q", tt.line, got)
			}
			// For whole-line colouring the colour code precedes the timestamp;
			// for token colouring it appears only at the level= field.
			startsColoured := strings.HasPrefix(got, tt.wantCode) || strings.HasPrefix(got, bold+tt.wantCode)
			if tt.whole != startsColoured {
				t.Errorf("colorizeLine(%q) whole=%v but startsColoured=%v: %q", tt.line, tt.whole, startsColoured, got)
			}
		})
	}
}

func TestColorizeLeavesNonLevelLinesAlone(t *testing.T) {
	line := "Checking 30 region(s) for upcoming deadlines…"
	if got := colorize(line); got != line {
		t.Errorf("expected non-level line unchanged, got %q", got)
	}
}

func TestNewWriterDisabledIsPassthrough(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, false)
	in := `time=t level=WARN msg="x"` + "\n"
	if _, err := w.Write([]byte(in)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != in {
		t.Errorf("disabled writer should pass through unchanged, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("disabled writer must not emit ANSI codes, got %q", buf.String())
	}
}

func TestNewWriterEnabledColorsAndReportsOriginalLength(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, true)
	in := []byte(`time=t level=ERROR msg="boom"` + "\n")
	n, err := w.Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(in) {
		t.Errorf("Write should report the original length %d, got %d", len(in), n)
	}
	if !strings.Contains(buf.String(), red) {
		t.Errorf("enabled writer should colour ERROR lines, got %q", buf.String())
	}
}
