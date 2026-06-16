package s3tui

import (
	"testing"
)

func multiHeaderModel() *Model {
	m := &Model{width: 100, height: 30, csvDelim: ',', csvRowCap: 0}
	m.csvAll = [][]string{
		{"report", "2026-06-16"}, // row 1: a header section's columns
		{"meta", "ignored"},      // row 2: that section's data
		{"id", "amount", "ccy"},  // row 3: the real txn columns
		{"1", "100", "AUD"},      // row 4: txn data
		{"2", "200", "USD"},
		{"3", "300", "GBP"},
	}
	return m
}

func TestHeaderRowSelection(t *testing.T) {
	m := multiHeaderModel()

	// Default: row 1 is the header.
	m.csvHeaderRow = 1
	h, d := m.headerAndData()
	if h[0] != "report" || len(d) != 5 {
		t.Errorf("default header: header=%v data=%d", h, len(d))
	}

	// Row 3 is the real header → txn data starts at row 4; earlier rows skipped.
	m.csvHeaderRow = 3
	h, d = m.headerAndData()
	if len(h) != 3 || h[0] != "id" || h[2] != "ccy" {
		t.Errorf("row-3 header = %v", h)
	}
	if len(d) != 3 || d[0][1] != "100" || d[2][2] != "GBP" {
		t.Errorf("row-3 data = %v", d)
	}

	// No header (0): synthesised names over the widest row; every row is data.
	m.csvHeaderRow = 0
	h, d = m.headerAndData()
	if len(h) != 3 || h[0] != "col 1" || h[2] != "col 3" {
		t.Errorf("no-header columns = %v", h)
	}
	if len(d) != 6 {
		t.Errorf("no-header should keep all %d rows as data, got %d", len(m.csvAll), len(d))
	}
}

func TestApplyHeaderRow(t *testing.T) {
	m := multiHeaderModel()
	m.csvHeaderRow = 1

	// Valid: set header to row 3.
	m.startCSVPrompt(csvPromptHeader)
	m.csvInput.SetValue("3")
	m.applyCSVPrompt()
	if m.csvPrompt != csvPromptNone || m.csvHeaderRow != 3 {
		t.Fatalf("apply 3: prompt=%v headerRow=%d err=%q", m.csvPrompt, m.csvHeaderRow, m.csvPromptErr)
	}
	if m.csvTotal != 3 {
		t.Errorf("after header=3, data rows = %d, want 3", m.csvTotal)
	}

	// 0 = no header.
	m.startCSVPrompt(csvPromptHeader)
	m.csvInput.SetValue("0")
	m.applyCSVPrompt()
	if m.csvPrompt != csvPromptNone || m.csvHeaderRow != 0 {
		t.Errorf("apply 0: prompt=%v headerRow=%d", m.csvPrompt, m.csvHeaderRow)
	}

	// Non-numeric and out-of-range keep the prompt open with an error.
	for _, bad := range []string{"abc", "99", "-1"} {
		m.startCSVPrompt(csvPromptHeader)
		m.csvInput.SetValue(bad)
		m.applyCSVPrompt()
		if m.csvPrompt == csvPromptNone || m.csvPromptErr == "" {
			t.Errorf("input %q should keep the prompt open with an error", bad)
		}
	}
}
