package s3tui

import (
	"encoding/csv"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// csvDelimiters are the candidate delimiters auto-detection chooses among, in
// preference order (earlier wins ties). They are also the values the "s" key
// cycles through so a user can override a wrong guess. \x1f is the ASCII unit
// separator, common in mainframe/extract ".dat" files.
var csvDelimiters = []rune{',', '\t', ';', '|', '\x1f'}

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
	csvPromptParquetRows
)

// colFilter selects which columns the table preview shows, for wide files where
// many columns are entirely empty. The default (colFilterAll) shows the dataset
// as-is; the other two narrow the view to populated or empty columns.
type colFilter int

const (
	colFilterAll      colFilter = iota // every column (default)
	colFilterWithData                  // only columns that contain data
	colFilterEmpty                     // only columns that are entirely empty
)

// colFilterLabel is the short phrase shown in the info line for each mode.
func colFilterLabel(f colFilter) string {
	switch f {
	case colFilterWithData:
		return "columns with data"
	case colFilterEmpty:
		return "empty columns"
	default:
		return "all columns"
	}
}

// colHasData reports whether any data row holds a non-blank value for column
// col. The window-divider rows (nil) are skipped, and emptiness is judged after
// trimming so spaces count as empty (the "no data at all" case).
func colHasData(data [][]string, col int) bool {
	for _, rec := range data {
		if rec == nil {
			continue
		}
		if col < len(rec) && strings.TrimSpace(rec[col]) != "" {
			return true
		}
	}
	return false
}

// filterColIndices returns the header column indices to display under filter f,
// computed over all data rows. A leading fixed-width "!" marker column (present
// only when hasMarker) is always kept — it is a status column, not data.
func filterColIndices(header []string, data [][]string, f colFilter, hasMarker bool) []int {
	out := make([]int, 0, len(header))
	for i := range header {
		if f == colFilterAll || (hasMarker && i == 0) {
			out = append(out, i)
			continue
		}
		has := colHasData(data, i)
		if (f == colFilterWithData && has) || (f == colFilterEmpty && !has) {
			out = append(out, i)
		}
	}
	return out
}

// dataColCount is the number of real data columns in idx, excluding a leading
// fixed-width marker column when hasMarker is set.
func dataColCount(idx []int, hasMarker bool) int {
	n := len(idx)
	if hasMarker {
		for _, i := range idx {
			if i == 0 {
				return n - 1
			}
		}
	}
	return n
}

// visibleColIndices returns the header indices to show under the active column
// filter for the current dataset.
func (m *Model) visibleColIndices(header []string, data [][]string) []int {
	return filterColIndices(header, data, m.csvColFilter, m.previewIsFixed)
}

// dataOrdinal is a column's 1-based position among the data columns (the number
// shown on the table's "(1) (2) …" line), independent of any column filtering.
// For fixed-width the leading "!" marker occupies header index 0, so a data
// column's ordinal is its header index; otherwise it is index+1.
func (m *Model) dataOrdinal(headerIdx int) int {
	if m.previewIsFixed {
		return headerIdx
	}
	return headerIdx + 1
}

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
	case ".csv", ".tsv", ".tab", ".dat", ".psv":
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
	m.previewIsParquet = false
	m.csvAll = recs
	m.csvHeaderRow = 1            // row 1 is the header by default (reset per file)
	m.csvColFilter = colFilterAll // show every column by default (reset per file)
	m.csvRecordActive = false
	if m.csvRowCap == 0 && !m.csvRowCapSet {
		m.csvRowCap = defaultCSVRowCap
		m.csvRowCapSet = true
	}
	m.buildCSVTable()
	return true
}

// initParquet loads the rows read from a Parquet object into the shared table.
// The schema column names become the header row, so the existing table, record
// view, row-window and copy-as-Markdown machinery all work unchanged.
func (m *Model) initParquet(header []string, rows [][]string, total int64) {
	all := make([][]string, 0, len(rows)+1)
	all = append(all, header)
	all = append(all, rows...)
	m.csvAll = all
	m.csvHeaderRow = 1            // the synthesised schema row is the header
	m.csvColFilter = colFilterAll // show every column by default (reset per file)
	m.csvDelim = ','              // unused for parquet, kept for a stable info line
	m.csvRecordActive = false
	m.parquetFileRows = total
	if m.csvRowCap == 0 && !m.csvRowCapSet {
		m.csvRowCap = defaultCSVRowCap
		m.csvRowCapSet = true
	}
	m.buildCSVTable()
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
		m.csvTable = table.New(table.WithStyles(ui.TableStylesZebra()))
		return
	}
	header, data := m.headerAndData()
	m.csvTotal = len(data)
	display, hidden := windowRecords(data, m.csvRowCap)
	m.csvHidden = hidden
	m.csvDisplay = display // keep the on-screen rows for the record view

	// Which columns to show under the active filter (emptiness judged over all
	// data, not just the on-screen window). Cached so the record and copy views
	// show the same columns.
	vis := m.visibleColIndices(header, data)
	// A delimiter/header change can leave an active filter matching no columns;
	// fall back to "all" so the table never renders blank.
	if m.csvColFilter != colFilterAll && dataColCount(vis, m.previewIsFixed) == 0 {
		m.csvColFilter = colFilterAll
		vis = m.visibleColIndices(header, data)
	}
	m.csvVisCols = vis

	cols := make([]table.Column, len(vis))
	for n, i := range vis {
		title := clipCell(strings.TrimSpace(header[i]))
		if title == "" {
			title = fmt.Sprintf("col %d", i+1)
		}
		col := table.Column{Title: title, Width: 4}
		// The fixed-width preview prepends a "!" malformed-row marker column; it
		// is not a data column, so exclude it from the "(1) (2) …" numbering and
		// let the first real column be (1).
		if m.previewIsFixed && i == 0 && title == fixedMarkerCol {
			col.NoNumber = true
		} else {
			// Show each column's original file position so filtering away the
			// empty columns doesn't renumber the survivors.
			col.Number = m.dataOrdinal(i)
		}
		cols[n] = col
	}

	rows := make([]table.Row, 0, len(display))
	for _, rec := range display {
		row := make(table.Row, len(vis))
		for n, i := range vis {
			if rec == nil {
				row[n] = "⋯"
			} else if i < len(rec) {
				row[n] = clipCell(rec[i])
			}
		}
		rows = append(rows, row)
	}

	m.csvTable = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1), // pin the first column when scrolling wide tables
		table.WithColNumbers(true), // show (1) (2) … under each header for wide files
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

// cycleCSVColFilter advances the column filter (all → with-data → empty → all)
// and rebuilds. A mode that would show no data columns is skipped, with a note,
// so the table never goes blank — e.g. "empty columns" is skipped when every
// column is populated.
func (m *Model) cycleCSVColFilter() {
	header, data := m.headerAndData()
	skipped := ""
	for step := 1; step <= 3; step++ {
		cand := colFilter((int(m.csvColFilter) + step) % 3)
		if cand != colFilterAll {
			idx := filterColIndices(header, data, cand, m.previewIsFixed)
			if dataColCount(idx, m.previewIsFixed) == 0 {
				skipped = "no " + colFilterLabel(cand)
				continue // never show a blank table
			}
		}
		m.csvColFilter = cand
		m.buildCSVTable()
		// Explain a skip only when we fell all the way back to "all".
		if cand == colFilterAll && skipped != "" {
			m.csvNote = skipped
		}
		return
	}
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
	case csvPromptParquetRows:
		ti.Prompt = "rows to read: "
		ti.SetValue(strconv.Itoa(m.parquetRows))
		ti.CursorEnd()
	}
	ti.Focus()
	m.csvInput = ti
	m.csvPrompt = kind
	m.csvPromptErr = ""
}

// applyCSVPrompt commits the open prompt; it stays open with an explanation on
// invalid input. It returns a command when the action needs follow-up work
// (the Parquet row count re-fetches from S3); otherwise nil.
func (m *Model) applyCSVPrompt() tea.Cmd {
	switch m.csvPrompt {
	case csvPromptDelim:
		m.applyDelimiter()
	case csvPromptHeader:
		m.applyHeaderRow()
	case csvPromptParquetRows:
		return m.applyParquetRows()
	}
	return nil
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

// applyParquetRows reads the requested row count and re-fetches the Parquet
// preview with the new window. Returns the fetch command on success.
func (m *Model) applyParquetRows() tea.Cmd {
	n, err := strconv.Atoi(strings.TrimSpace(m.csvInput.Value()))
	if err != nil || n <= 0 {
		m.csvPromptErr = "enter a positive number of rows"
		return nil
	}
	if n > parquetMaxRowsLimit {
		m.csvPromptErr = fmt.Sprintf("max %d rows", parquetMaxRowsLimit)
		return nil
	}
	m.parquetRows = n
	m.csvPrompt = csvPromptNone
	m.csvPromptErr = ""
	return m.fetchParquetPreview(m.previewKey)
}

// handleCSVKey routes a key press in the full-screen CSV view. It returns true
// when the key was consumed.
func (m *Model) handleCSVKey(key string) bool {
	// Any key clears a lingering confirmation note (e.g. after a copy).
	m.csvNote = ""
	switch key {
	case "y":
		m.copyCSVAsMarkdown()
		return true
	case "esc", "q":
		m.showCSV = false
		m.csvAll = nil
		m.previewIsParquet = false
		m.previewIsFixed = false
		// A member opened from an archive returns to the member list.
		if m.previewFromArchive {
			m.previewFromArchive = false
			m.showArchive = true
		}
		return true
	case "t":
		// Parquet is binary — there is no raw-text view to toggle to.
		if m.previewIsParquet {
			return true
		}
		// Toggle to the raw-text preview of the same object.
		m.previewIsFixed = false
		m.showCSV = false
		m.showPreview = true
		m.initPreviewViewport(m.previewContent, m.previewErr)
		return true
	case "L":
		// Re-apply (or apply) a local fixed-width layout file to the same object.
		if m.previewIsParquet {
			return true // a typed Parquet schema needs no positional layout
		}
		m.startLayoutPrompt()
		return true
	case "s":
		if m.previewIsParquet || m.previewIsFixed {
			return true // delimiter is meaningless for a typed/positional schema
		}
		m.cycleCSVDelimiter()
		return true
	case "S":
		if m.previewIsParquet || m.previewIsFixed {
			return true
		}
		m.startCSVPrompt(csvPromptDelim)
		return true
	case "h":
		if m.previewIsParquet || m.previewIsFixed {
			return true // the layout defines the columns; the header is fixed
		}
		m.startCSVPrompt(csvPromptHeader)
		return true
	case "n":
		// Parquet only: re-fetch a different number of rows from the file.
		if m.previewIsParquet {
			m.startCSVPrompt(csvPromptParquetRows)
			return true
		}
		return false
	case "w":
		m.cycleCSVRowCap()
		return true
	case "c":
		// Cycle the column filter: all → only-with-data → only-empty → all.
		m.cycleCSVColFilter()
		return true
	case "left", "<", ",":
		m.csvTable.ScrollLeft()
		return true
	case "right", ">", ".":
		m.csvTable.ScrollRight()
		return true
	case "enter":
		m.openCSVRecord()
		return true
	}
	return false
}

// copyCSVAsMarkdown copies the on-screen table — the header plus the rows in the
// current window (so the copy matches what the "w" window shows) — to the
// clipboard as a GitHub-flavored Markdown table.
func (m *Model) copyCSVAsMarkdown() {
	header, _ := m.headerAndData()
	// Copy exactly the columns the table shows (honoring the column filter).
	vh, vrows := projectCols(header, m.csvDisplay, m.csvVisCols)
	md, rows := csvMarkdown(vh, vrows)
	if md == "" {
		m.csvNote = "Nothing to copy"
		return
	}
	if err := clipboard.WriteAll(md); err != nil {
		m.csvNote = "Copy failed: " + err.Error()
		return
	}
	m.csvNote = fmt.Sprintf("Copied %d rows as Markdown", rows)
}

// csvMarkdown renders a header and its window rows as a GitHub-flavored Markdown
// table, returning the text and the number of data rows written. nil rows (the
// first/last window divider) are skipped. Pure, so it is unit-tested.
func csvMarkdown(header []string, display [][]string) (string, int) {
	if len(header) == 0 {
		return "", 0
	}
	cols := make([]string, len(header))
	seps := make([]string, len(header))
	for i, h := range header {
		if cols[i] = mdCell(h); cols[i] == "" {
			cols[i] = fmt.Sprintf("col %d", i+1)
		}
		seps[i] = "---"
	}

	var b strings.Builder
	b.WriteString("| " + strings.Join(cols, " | ") + " |\n")
	b.WriteString("| " + strings.Join(seps, " | ") + " |\n")
	rows := 0
	for _, rec := range display {
		if rec == nil {
			continue // the first/last window divider, not a real row
		}
		cells := make([]string, len(cols))
		for i := range cols {
			if i < len(rec) {
				cells[i] = mdCell(rec[i])
			}
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		rows++
	}
	return b.String(), rows
}

// projectCols reduces header and rows to the given column indices, preserving
// order, so the record and copy views show exactly the table's filtered columns.
// Window-divider rows (nil) are preserved as nil.
func projectCols(header []string, rows [][]string, cols []int) ([]string, [][]string) {
	h := make([]string, len(cols))
	for n, i := range cols {
		if i >= 0 && i < len(header) {
			h[n] = header[i]
		}
	}
	out := make([][]string, len(rows))
	for r, rec := range rows {
		if rec == nil {
			continue // leave nil to mark the window divider
		}
		row := make([]string, len(cols))
		for n, i := range cols {
			if i >= 0 && i < len(rec) {
				row[n] = rec[i]
			}
		}
		out[r] = row
	}
	return h, out
}

// mdCell makes a value safe for one Markdown table cell: pipes escaped, newlines
// and tabs flattened to spaces so the row stays on one line.
func mdCell(s string) string {
	r := strings.NewReplacer("\r", "", "\n", " ", "\t", " ", "|", "\\|")
	return strings.TrimSpace(r.Replace(s))
}

// openCSVRecord shows the selected table row vertically as Col : value pairs —
// far easier to read than horizontally scrolling a wide row.
func (m *Model) openCSVRecord() {
	i := m.csvTable.Cursor()
	if i < 0 || i >= len(m.csvDisplay) {
		return
	}
	rec := m.csvDisplay[i]
	if rec == nil {
		return // the elision divider between the first/last windows
	}
	header, _ := m.headerAndData()

	// Show the same columns the table shows (honoring the column filter), minus
	// any fixed-width "!" marker column — that is a row-status flag, not a field.
	cols := make([]int, 0, len(m.csvVisCols))
	for _, h := range m.csvVisCols {
		if m.previewIsFixed && h == 0 {
			continue
		}
		cols = append(cols, h)
	}

	// Width to which column names are padded, so the colons line up.
	labelW := 0
	for _, h := range cols {
		name := ""
		if h < len(header) {
			name = strings.TrimSpace(header[h])
		}
		if w := len([]rune(name)); w > labelW {
			labelW = w
		}
	}
	if labelW > 32 {
		labelW = 32
	}

	// Leading column number ("1:", "2:" …) is the column's original file
	// position, right-aligned so the colons line up; sized to the largest shown.
	maxOrd := 0
	for _, h := range cols {
		if o := m.dataOrdinal(h); o > maxOrd {
			maxOrd = o
		}
	}
	seqW := len(strconv.Itoa(maxOrd))
	if seqW < 1 {
		seqW = 1
	}
	var b strings.Builder
	for _, h := range cols {
		name := ""
		if h < len(header) {
			name = strings.TrimSpace(header[h])
		}
		if name == "" {
			name = fmt.Sprintf("col %d", h+1)
		}
		if r := []rune(name); len(r) > labelW {
			name = string(r[:labelW])
		}
		val := ""
		if h < len(rec) {
			val = strings.ReplaceAll(rec[h], "\r", "")
		}
		fmt.Fprintf(&b, "%*d: %-*s : %s\n", seqW, m.dataOrdinal(h), labelW, name, val)
	}

	vpW := m.tableViewWidth()
	vpH := m.height - 8
	if vpH < 3 {
		vpH = 3
	}
	m.csvRecordViewport = viewport.New(vpW, vpH)
	m.csvRecordViewport.SetContent(hardWrap(strings.TrimRight(b.String(), "\n"), vpW))
	m.csvRecordIndex = i
	m.csvRecordActive = true
}

// csvView renders the full-screen CSV table window (or the single-row record
// view when active).
func (m *Model) csvView() string {
	label, loading := "CSV TABLE: ", "Loading CSV…"
	if m.previewIsParquet {
		label, loading = "PARQUET: ", "Reading Parquet…"
	} else if m.previewIsFixed {
		label = "FIXED-WIDTH: "
	}
	title := ui.PanelTitleStyle().Render(label + m.previewKey)

	if m.previewLoading {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.loadingLine(loading))
	}
	if m.csvRecordActive {
		return m.csvRecordView()
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
	if m.enteringLayout {
		return layoutPromptLine(m.layoutInput.View(), m.layoutErr)
	}
	if m.csvPrompt != csvPromptNone {
		line := m.csvInput.View() + ui.MutedStyle().Render("   Enter apply · Esc cancel")
		if m.csvPromptErr != "" {
			line += "   " + ui.ErrorStyle().Render(m.csvPromptErr)
		}
		return line
	}
	if m.csvNote != "" {
		return ui.SuccessStyle().Render("✓ " + m.csvNote)
	}
	if m.previewIsParquet {
		return ui.MutedStyle().Render(
			"[↑/↓ PgUp/PgDn] rows   [Enter] row as record   [←/→] columns   [c] col filter   [n] rows to read   [w] window   [y] copy as Markdown   [Esc] close")
	}
	if m.previewIsFixed {
		return ui.MutedStyle().Render(
			"[↑/↓ PgUp/PgDn] rows   [Enter] row as record   [←/→] columns   [c] col filter   [w] window   [y] copy as Markdown   [L] re-apply layout   [t] raw   [Esc] close")
	}
	return ui.MutedStyle().Render(
		"[↑/↓ PgUp/PgDn] rows   [Enter] row as record   [←/→] columns   [c] col filter   [s]/[S] delimiter   [h] header row   [w] rows   [y] copy as Markdown   [t] raw   [Esc] close")
}

// layoutPromptLine renders the shared local-layout-file prompt with apply/cancel
// hints and any error.
func layoutPromptLine(inputView, errMsg string) string {
	line := inputView + ui.MutedStyle().Render("   Enter apply · Esc cancel")
	if errMsg != "" {
		line += "   " + ui.ErrorStyle().Render(errMsg)
	}
	return line
}

// csvRecordView renders the selected row vertically as Col : value pairs.
func (m *Model) csvRecordView() string {
	header, _ := m.headerAndData()
	title := ui.PanelTitleStyle().Render("RECORD: " + m.previewKey)
	info := ui.MutedStyle().Render(fmt.Sprintf("row %d   ·   %d columns", m.csvRecordIndex+1, len(header)))
	bar := ui.VScrollbar(
		m.csvRecordViewport.Height,
		m.csvRecordViewport.TotalLineCount(),
		m.csvRecordViewport.VisibleLineCount(),
		m.csvRecordViewport.YOffset,
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.csvRecordViewport.View(), " ", bar)
	hints := ui.MutedStyle().Render("[↑/↓ PgUp/PgDn] scroll   [Esc] back to table")
	return lipgloss.JoinVertical(lipgloss.Left, title, info, ui.TablePanelStyle(true).Render(body), hints)
}

// csvInfoLine summarises the delimiter, header row, the column window and the
// row window.
func (m *Model) csvInfoLine() string {
	header, _ := m.headerAndData()
	cols := len(header)
	if m.previewIsParquet {
		return m.parquetInfoLine(cols)
	}
	if m.previewIsFixed {
		return m.fixedInfoLine(cols)
	}
	// Make horizontal scrolling discoverable and report the column filter.
	colsPart := m.colsSummary(header, cols)
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

// colsSummary is the column dimension of the info line, shared by the CSV,
// Parquet and fixed-width views. totalData is the number of data columns in the
// file (excluding any fixed-width marker column). It reports the active column
// filter and count, and — when the table is scrolled horizontally — which
// columns are on screen.
func (m *Model) colsSummary(header []string, totalData int) string {
	kept := dataColCount(m.csvVisCols, m.previewIsFixed)
	if hl, hr := m.csvTable.ColScrollInfo(); hl+hr > 0 {
		out := m.colWindowInfo(header, totalData)
		if m.csvColFilter != colFilterAll {
			out += "  ·  " + colFilterLabel(m.csvColFilter)
		}
		return out
	}
	if m.csvColFilter != colFilterAll {
		return fmt.Sprintf("%d of %d %s", kept, totalData, colFilterLabel(m.csvColFilter))
	}
	return fmt.Sprintf("%d columns", kept)
}

// colWindowInfo describes which columns are on screen when the table is wider
// than the view: the visible scrollable range, the total data-column count, and
// the name of the leftmost scrollable column as a "where am I" anchor for very
// wide files. The on-screen positions (1-based into the shown columns) are
// mapped back through csvVisCols to each column's original data ordinal, so the
// numbers match the "(1) (2) …" header line even when a fixed-width marker
// column is pinned or empty columns have been filtered out.
func (m *Model) colWindowInfo(header []string, totalData int) string {
	lo, hi, ok := m.csvTable.VisibleScrollableCols()
	if !ok {
		return fmt.Sprintf("%d columns", totalData)
	}
	loOrd, anchor := m.shownColInfo(header, lo)
	hiOrd, _ := m.shownColInfo(header, hi)
	out := fmt.Sprintf("cols %d-%d of %d (←/→ for more)", loOrd, hiOrd, totalData)
	if anchor != "" {
		out += fmt.Sprintf("  ·  col %d: %s", loOrd, anchor)
	}
	return out
}

// shownColInfo maps a 1-based position among the shown columns to that column's
// original data ordinal and trimmed name.
func (m *Model) shownColInfo(header []string, shownPos int) (ordinal int, name string) {
	i := shownPos - 1
	if i < 0 || i >= len(m.csvVisCols) {
		return shownPos, ""
	}
	h := m.csvVisCols[i]
	ordinal = m.dataOrdinal(h)
	if h >= 0 && h < len(header) {
		name = clipCell(strings.TrimSpace(header[h]))
	}
	return ordinal, name
}

// parquetInfoLine summarises the schema width and the read window for a Parquet
// preview, making clear how many of the file's rows are shown.
func (m *Model) parquetInfoLine(cols int) string {
	header, _ := m.headerAndData()
	colsPart := m.colsSummary(header, cols)
	// csvTotal is the number of rows read; parquetFileRows is the file's total.
	rowsPart := fmt.Sprintf("%d rows", m.csvTotal)
	if m.parquetFileRows > int64(m.csvTotal) {
		rowsPart = fmt.Sprintf("first %d of %d rows ([n] to change)", m.csvTotal, m.parquetFileRows)
	}
	if m.csvHidden > 0 {
		rowsPart = fmt.Sprintf("first %d + last %d of %d read (%d hidden, %d in file)",
			m.csvRowCap, m.csvRowCap, m.csvTotal, m.csvHidden, m.parquetFileRows)
	}
	return fmt.Sprintf("parquet   ·   %s   ·   %s", colsPart, rowsPart)
}
