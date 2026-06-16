package s3tui

import (
	"encoding/csv"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// csvDelimiters are the candidate delimiters auto-detection chooses among, in
// preference order (earlier wins ties). They are also the values the "s" key
// cycles through so a user can override a wrong guess.
var csvDelimiters = []rune{',', '\t', ';', '|'}

// csvRowCaps are the first-N/last-N row windows the "w" key cycles through. 0
// means "all rows". A large CSV preview shows the first cap and last cap rows
// with a divider between, keeping the table responsive.
var csvRowCaps = []int{100, 500, 1000, 0}

// csvCellCap bounds a cell's displayed width so one long value can't blow a
// column up; the table scrolls horizontally for the rest.
const csvCellCap = 60

func delimiterName(r rune) string {
	switch r {
	case ',':
		return "comma"
	case '\t':
		return "tab"
	case ';':
		return "semicolon"
	case '|':
		return "pipe"
	default:
		return fmt.Sprintf("%q", string(r))
	}
}

// looksLikeCSV reports whether a key's extension marks it as delimited text.
func looksLikeCSV(key string) bool {
	switch strings.ToLower(path.Ext(key)) {
	case ".csv", ".tsv", ".tab":
		return true
	}
	return false
}

// detectDelimiter picks the candidate that splits the sample into the most
// columns most consistently, defaulting to comma when nothing stands out.
func detectDelimiter(content string) rune {
	lines := firstLines(content, 20)
	best, bestScore := ',', -1
	for _, d := range csvDelimiters {
		if s := delimiterScore(lines, d); s > bestScore {
			best, bestScore = d, s
		}
	}
	return best
}

// delimiterScore rewards a delimiter that appears the same number of times on
// the most lines: modal-count × how-many-lines-share-it.
func delimiterScore(lines []string, d rune) int {
	freq := map[int]int{}
	for _, ln := range lines {
		if c := strings.Count(ln, string(d)); c > 0 {
			freq[c]++
		}
	}
	modal, modalFreq := 0, 0
	for c, f := range freq {
		if f > modalFreq || (f == modalFreq && c > modal) {
			modal, modalFreq = c, f
		}
	}
	return modal * modalFreq
}

func firstLines(content string, n int) []string {
	var out []string
	for _, ln := range strings.Split(content, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
		if len(out) >= n {
			break
		}
	}
	return out
}

// parseCSV parses content with the given delimiter, tolerating ragged rows and
// a truncated/malformed tail (the preview is only the first chunk of the
// object). It reports ok only when the result is table-shaped: at least a
// header plus one data row, and at least two columns.
func parseCSV(content string, delim rune) ([][]string, bool) {
	r := csv.NewReader(strings.NewReader(content))
	r.Comma = delim
	r.FieldsPerRecord = -1 // allow ragged rows
	r.LazyQuotes = true
	var recs [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // keep the complete rows parsed before a truncated tail
		}
		recs = append(recs, rec)
	}
	if len(recs) < 2 || len(recs[0]) < 2 {
		return nil, false
	}
	return recs, true
}

// clipCell flattens embedded newlines/tabs (which would corrupt table rows) and
// caps the cell to csvCellCap display runes.
func clipCell(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	r := []rune(s)
	if len(r) > csvCellCap {
		return string(r[:csvCellCap-1]) + "…"
	}
	return s
}

// windowRecords returns the data rows to display for the given cap (0 = all). A
// nil entry marks the elision divider between the head and tail windows; hidden
// is the number of rows omitted.
func windowRecords(data [][]string, cap int) (display [][]string, hidden int) {
	if cap <= 0 || len(data) <= 2*cap {
		return data, 0
	}
	display = append(display, data[:cap]...)
	display = append(display, nil) // divider
	display = append(display, data[len(data)-cap:]...)
	return display, len(data) - 2*cap
}

// initCSV parses the preview content and builds the table; ok is false when the
// content is not table-shaped, so the caller can fall back to the text preview.
func (m *Model) initCSV(content string) bool {
	m.csvDelim = detectDelimiter(content)
	recs, ok := parseCSV(content, m.csvDelim)
	if !ok {
		return false
	}
	m.csvAll = recs
	if m.csvRowCap == 0 && !m.csvRowCapSet {
		m.csvRowCap = defaultCSVRowCap
		m.csvRowCapSet = true
	}
	m.buildCSVTable()
	return true
}

const defaultCSVRowCap = 100

// buildCSVTable (re)builds the shared table from the parsed records, applying
// the current row window. Safe to call after a delimiter or window change.
func (m *Model) buildCSVTable() {
	if len(m.csvAll) == 0 {
		m.csvTable = table.New(table.WithStyles(ui.TableStyles()))
		return
	}
	header := m.csvAll[0]
	data := m.csvAll[1:]
	m.csvTotal = len(data)
	display, hidden := windowRecords(data, m.csvRowCap)
	m.csvHidden = hidden

	cols := make([]table.Column, len(header))
	for i, h := range header {
		title := clipCell(strings.TrimSpace(h))
		if title == "" {
			title = fmt.Sprintf("col %d", i+1)
		}
		cols[i] = table.Column{Title: title, Width: 4}
	}

	rows := make([]table.Row, 0, len(display))
	for _, rec := range display {
		row := make(table.Row, len(header))
		for i := range header {
			if rec == nil {
				row[i] = "⋯"
			} else if i < len(rec) {
				row[i] = clipCell(rec[i])
			}
		}
		rows = append(rows, row)
	}

	m.csvTable = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1), // pin the first column when scrolling wide tables
	)
	m.layoutCSVTable()
}

// layoutCSVTable sizes the CSV table to fill the full-screen window.
func (m *Model) layoutCSVTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.csvTable.SetWidth(m.tableViewWidth())
	h := m.height - 9 // app margins, title, info line, panel border, footer
	if h < 3 {
		h = 3
	}
	m.csvTable.SetHeight(h)
}

// cycleCSVDelimiter advances to the next delimiter and reparses; if the new
// delimiter doesn't yield a table the previous one is kept.
func (m *Model) cycleCSVDelimiter() {
	start := indexRune(csvDelimiters, m.csvDelim)
	for i := 1; i <= len(csvDelimiters); i++ {
		d := csvDelimiters[(start+i)%len(csvDelimiters)]
		if recs, ok := parseCSV(m.previewContent, d); ok {
			m.csvDelim = d
			m.csvAll = recs
			m.buildCSVTable()
			return
		}
	}
}

// cycleCSVRowCap advances the first-N/last-N window size and rebuilds.
func (m *Model) cycleCSVRowCap() {
	idx := 0
	for i, c := range csvRowCaps {
		if c == m.csvRowCap {
			idx = i
			break
		}
	}
	m.csvRowCap = csvRowCaps[(idx+1)%len(csvRowCaps)]
	m.csvRowCapSet = true
	m.buildCSVTable()
}

func indexRune(rs []rune, r rune) int {
	for i, x := range rs {
		if x == r {
			return i
		}
	}
	return 0
}

// handleCSVKey routes a key press in the full-screen CSV view. It returns true
// when the key was consumed.
func (m *Model) handleCSVKey(key string) bool {
	switch key {
	case "esc", "q":
		m.showCSV = false
		m.csvAll = nil
		// A member opened from an archive returns to the member list.
		if m.previewFromArchive {
			m.previewFromArchive = false
			m.showArchive = true
		}
		return true
	case "t":
		// Toggle to the raw-text preview of the same object.
		m.showCSV = false
		m.showPreview = true
		m.initPreviewViewport(m.previewContent, m.previewErr)
		return true
	case "s":
		m.cycleCSVDelimiter()
		return true
	case "w":
		m.cycleCSVRowCap()
		return true
	case "left", "<", ",":
		m.csvTable.ScrollLeft()
		return true
	case "right", ">", ".":
		m.csvTable.ScrollRight()
		return true
	}
	return false
}

// csvView renders the full-screen CSV table window.
func (m *Model) csvView() string {
	title := ui.PanelTitleStyle().Render("CSV TABLE: " + m.previewKey)

	if m.previewLoading {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.loadingLine("Loading CSV…"))
	}
	if m.previewErr != nil {
		return lipgloss.JoinVertical(lipgloss.Left, title, "",
			ui.ErrorStyle().Render("Preview failed: "+summarizeS3Error(m.previewErr)))
	}

	info := ui.MutedStyle().Render(m.csvInfoLine())
	panel := ui.TablePanelStyle(true).Render(m.csvTable.View())
	scroll := ui.TableScrollIndicator(&m.csvTable)
	hints := ui.MutedStyle().Render(
		"[↑/↓ PgUp/PgDn] rows   [←/→] columns   [s] delimiter   [w] rows shown   [t] raw text   [Esc] close")

	parts := []string{title, info, panel}
	if scroll != "" {
		parts = append(parts, scroll)
	}
	parts = append(parts, hints)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// csvInfoLine summarises the delimiter, column count and the row window.
func (m *Model) csvInfoLine() string {
	cols := 0
	if len(m.csvAll) > 0 {
		cols = len(m.csvAll[0])
	}
	rowsPart := fmt.Sprintf("%d rows", m.csvTotal)
	if m.csvHidden > 0 {
		rowsPart = fmt.Sprintf("first %d + last %d of %d rows (%d hidden)",
			m.csvRowCap, m.csvRowCap, m.csvTotal, m.csvHidden)
	}
	return fmt.Sprintf("delimiter: %s   ·   %d columns   ·   %s", delimiterName(m.csvDelim), cols, rowsPart)
}
