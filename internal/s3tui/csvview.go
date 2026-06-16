package s3tui

import (
	"encoding/csv"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
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

// csvPromptKind is what the shared typed prompt is currently editing.
type csvPromptKind int

const (
	csvPromptNone csvPromptKind = iota
	csvPromptDelim
	csvPromptHeader
)

func maxCols(recs [][]string) int {
	n := 0
	for _, r := range recs {
		if len(r) > n {
			n = len(r)
		}
	}
	return n
}

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
	case ' ':
		return "space"
	}
	// Control characters (e.g. ASCII 31 unit separator) have no printable glyph.
	if r < 0x20 || r == 0x7f {
		return fmt.Sprintf("ASCII %d (0x%02x)", r, r)
	}
	return fmt.Sprintf("%q", string(r))
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
	m.csvHeaderRow = 1 // row 1 is the header by default (reset per file)
	if m.csvRowCap == 0 && !m.csvRowCapSet {
		m.csvRowCap = defaultCSVRowCap
		m.csvRowCapSet = true
	}
	m.buildCSVTable()
	return true
}

// headerAndData splits the parsed records into the header row and the data rows
// per csvHeaderRow (1-based; 0 = no header, so column names are synthesised and
// every row is data). Rows before the header row are skipped — handy for files
// that prepend their own header section before the real columns.
func (m *Model) headerAndData() (header []string, data [][]string) {
	if m.csvHeaderRow <= 0 {
		n := maxCols(m.csvAll)
		header = make([]string, n)
		for i := range header {
			header[i] = fmt.Sprintf("col %d", i+1)
		}
		return header, m.csvAll
	}
	idx := m.csvHeaderRow - 1
	if idx >= len(m.csvAll) {
		idx = len(m.csvAll) - 1
	}
	return m.csvAll[idx], m.csvAll[idx+1:]
}

const defaultCSVRowCap = 100

// buildCSVTable (re)builds the shared table from the parsed records, applying
// the current row window. Safe to call after a delimiter or window change.
func (m *Model) buildCSVTable() {
	if len(m.csvAll) == 0 {
		m.csvTable = table.New(table.WithStyles(ui.TableStyles()))
		return
	}
	header, data := m.headerAndData()
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

// parseDelimiterSpec interprets a user-typed delimiter so unusual separators
// (a tab, ASCII 31 unit-separator, etc.) can be entered in the UI. It accepts:
//   - a single literal character: ; | # …
//   - an escape: \t, \xNN / \uNNNN (hex), \\ (backslash)
//   - a decimal code: 31, 9 …
//   - a name: tab, comma, semicolon, pipe, space, unit/us
//
// Returns the rune and whether it was understood and is usable as a delimiter.
func parseDelimiterSpec(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	switch strings.ToLower(s) {
	case "tab", `\t`:
		return '\t', true
	case "comma":
		return ',', true
	case "semicolon", "semi":
		return ';', true
	case "pipe", "bar":
		return '|', true
	case "space":
		return ' ', true
	case "unit", "us":
		return '\x1f', true
	}
	switch {
	case strings.HasPrefix(s, `\x`), strings.HasPrefix(s, `\X`),
		strings.HasPrefix(s, `\u`), strings.HasPrefix(s, `\U`):
		if n, err := strconv.ParseInt(s[2:], 16, 32); err == nil {
			return rune(n), validDelimRune(rune(n))
		}
		return 0, false
	case s == `\\`:
		return '\\', true
	case len(s) > 1 && isAllDigits(s):
		if n, err := strconv.Atoi(s); err == nil {
			return rune(n), validDelimRune(rune(n))
		}
		return 0, false
	}
	r := []rune(s)
	if len(r) == 1 {
		return r[0], validDelimRune(r[0])
	}
	return 0, false
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// validDelimRune rejects runes that encoding/csv won't accept as a field
// delimiter.
func validDelimRune(r rune) bool {
	return r > 0 && r != '\r' && r != '\n' && r != utf8.RuneError && r != '\ufeff'
}

// startCSVPrompt opens the shared typed prompt for the given action.
func (m *Model) startCSVPrompt(kind csvPromptKind) {
	ti := textinput.New()
	ti.CharLimit = 12
	ti.Width = 22
	switch kind {
	case csvPromptDelim:
		ti.Prompt = "delimiter: "
		ti.Placeholder = `,  \t  \x1f  ;  31`
	case csvPromptHeader:
		ti.Prompt = "header row (0 = none): "
		ti.SetValue(strconv.Itoa(m.csvHeaderRow))
		ti.CursorEnd()
	}
	ti.Focus()
	m.csvInput = ti
	m.csvPrompt = kind
	m.csvPromptErr = ""
}

// applyCSVPrompt commits the open prompt; it stays open with an explanation on
// invalid input.
func (m *Model) applyCSVPrompt() {
	switch m.csvPrompt {
	case csvPromptDelim:
		m.applyDelimiter()
	case csvPromptHeader:
		m.applyHeaderRow()
	}
}

func (m *Model) applyDelimiter() {
	spec := strings.TrimSpace(m.csvInput.Value())
	r, ok := parseDelimiterSpec(spec)
	if !ok {
		m.csvPromptErr = "unrecognised delimiter — try , \\t \\x1f or 31"
		return
	}
	recs, parsed := parseCSV(m.previewContent, r)
	if !parsed {
		m.csvPromptErr = "no table found with delimiter " + delimiterName(r)
		return
	}
	m.csvDelim = r
	m.csvAll = recs
	if m.csvHeaderRow > len(m.csvAll) {
		m.csvHeaderRow = 1
	}
	m.buildCSVTable()
	m.csvPrompt = csvPromptNone
	m.csvPromptErr = ""
}

func (m *Model) applyHeaderRow() {
	n, err := strconv.Atoi(strings.TrimSpace(m.csvInput.Value()))
	if err != nil || n < 0 {
		m.csvPromptErr = "enter a row number (0 = no header)"
		return
	}
	if n > len(m.csvAll) {
		m.csvPromptErr = fmt.Sprintf("only %d row(s) in the file", len(m.csvAll))
		return
	}
	m.csvHeaderRow = n
	m.buildCSVTable()
	m.csvPrompt = csvPromptNone
	m.csvPromptErr = ""
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
	case "S":
		m.startCSVPrompt(csvPromptDelim)
		return true
	case "h":
		m.startCSVPrompt(csvPromptHeader)
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

	parts := []string{title, info, panel}
	if scroll != "" {
		parts = append(parts, scroll)
	}
	parts = append(parts, m.csvFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// csvFooter is the typed prompt while editing, otherwise the key hints.
func (m *Model) csvFooter() string {
	if m.csvPrompt != csvPromptNone {
		line := m.csvInput.View() + ui.MutedStyle().Render("   Enter apply · Esc cancel")
		if m.csvPromptErr != "" {
			line += "   " + ui.ErrorStyle().Render(m.csvPromptErr)
		}
		return line
	}
	return ui.MutedStyle().Render(
		"[↑/↓ PgUp/PgDn] rows   [←/→] columns   [s]/[S] delimiter   [h] header row   [w] rows shown   [t] raw text   [Esc] close")
}

// csvInfoLine summarises the delimiter, header row, the column window and the
// row window.
func (m *Model) csvInfoLine() string {
	header, _ := m.headerAndData()
	cols := len(header)
	// Make horizontal scrolling discoverable: when columns are off-screen, say
	// how many are shown and how to reach the rest.
	colsPart := fmt.Sprintf("%d columns", cols)
	if hl, hr := m.csvTable.ColScrollInfo(); hl+hr > 0 {
		colsPart = fmt.Sprintf("%d of %d columns shown (←/→ for more)", cols-hl-hr, cols)
	}
	headerPart := fmt.Sprintf("header: row %d", m.csvHeaderRow)
	if m.csvHeaderRow == 0 {
		headerPart = "header: none"
	}
	rowsPart := fmt.Sprintf("%d rows", m.csvTotal)
	if m.csvHidden > 0 {
		rowsPart = fmt.Sprintf("first %d + last %d of %d rows (%d hidden)",
			m.csvRowCap, m.csvRowCap, m.csvTotal, m.csvHidden)
	}
	return fmt.Sprintf("delimiter: %s   ·   %s   ·   %s   ·   %s",
		delimiterName(m.csvDelim), headerPart, colsPart, rowsPart)
}
