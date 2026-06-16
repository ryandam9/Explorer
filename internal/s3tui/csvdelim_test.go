package s3tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseDelimiterSpec(t *testing.T) {
	ok := map[string]rune{
		",": ',', ";": ';', "|": '|', "#": '#',
		`\t`: '\t', "tab": '\t', "TAB": '\t',
		`\x1f`: 0x1f, `\X1F`: 0x1f, ``: 0x1f,
		"31": 0x1f, "9": '9', "124": '|',
		"unit": 0x1f, "us": 0x1f, "space": ' ', "comma": ',',
		`\\`: '\\',
	}
	for in, want := range ok {
		got, valid := parseDelimiterSpec(in)
		if !valid || got != want {
			t.Errorf("parseDelimiterSpec(%q) = %q,%v want %q", in, string(got), valid, string(want))
		}
	}
	for _, in := range []string{"", "abc", `\xZZ`, "\n", "\r"} {
		if _, valid := parseDelimiterSpec(in); valid {
			t.Errorf("parseDelimiterSpec(%q) should be invalid", in)
		}
	}
}

func TestApplyDelimiterInputUnitSeparator(t *testing.T) {
	// A unit-separator (ASCII 31) delimited file.
	us := "\x1f"
	var b strings.Builder
	b.WriteString("id" + us + "name" + us + "city\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%d%sUser %d%sSydney\n", i, us, i, us)
	}
	m := &Model{width: 100, height: 24, previewContent: b.String(), showCSV: true}
	// Auto-detect would pick comma and fail to find a table; set it manually.
	m.startCSVPrompt(csvPromptDelim)
	m.csvInput.SetValue(`\x1f`)
	m.applyCSVPrompt()

	if m.csvPrompt != csvPromptNone {
		t.Fatalf("prompt should close on success; err=%q", m.csvPromptErr)
	}
	if m.csvDelim != 0x1f {
		t.Errorf("delim = %q, want unit separator", string(m.csvDelim))
	}
	if len(m.csvAll) != 6 || len(m.csvAll[0]) != 3 {
		t.Errorf("parsed shape wrong: %d rows, %d cols", len(m.csvAll), len(m.csvAll[0]))
	}
	if !strings.Contains(m.csvInfoLine(), "ASCII 31") {
		t.Errorf("info line should name the delimiter: %q", m.csvInfoLine())
	}

	// An unparseable delimiter keeps the prompt open with an error.
	m.startCSVPrompt(csvPromptDelim)
	m.csvInput.SetValue("@")
	m.applyCSVPrompt()
	if m.csvPrompt == csvPromptNone || m.csvPromptErr == "" {
		t.Error("a delimiter that yields no table should keep the prompt open with an error")
	}
}
