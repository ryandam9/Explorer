package s3tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
)

// fixedField is one column of a fixed-width (positional) record: a name, a
// 1-based byte start offset, and a byte length. Fixed-width / "flat" files
// (mainframe extracts, COBOL copybook data) have no delimiter — each column
// begins at a fixed position and runs for a fixed number of bytes.
type fixedField struct {
	name   string
	start  int // 1-based byte offset of the first byte
	length int // number of bytes
}

// width returns the byte offset one past the field's last byte.
func (f fixedField) width() int { return f.start - 1 + f.length }

// layoutFileCap bounds how large a local layout file may be. A layout is a
// short spec (one line per column), so anything larger is almost certainly the
// wrong file.
const layoutFileCap = 256 * 1024

// fixedMarkerCol is the header of the leading status column that flags rows
// whose byte length does not match the layout. ASCII is used deliberately so
// it renders in every terminal (the table headers avoid non-ASCII glyphs).
const fixedMarkerCol = "!"

// readLayoutFile reads a user-supplied local layout file, expanding a leading
// "~/" and stripping a UTF-8 BOM. It returns a friendly error for the common
// mistakes (missing path, a directory, an oversized file).
func readLayoutFile(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, path[2:])
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("layout file not found: %s", path)
		}
		return "", fmt.Errorf("cannot read layout file: %v", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a layout file", path)
	}
	if info.Size() > layoutFileCap {
		return "", fmt.Errorf("layout file is too large (> %d KiB)", layoutFileCap/1024)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read layout file: %v", err)
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}) // strip UTF-8 BOM
	return string(b), nil
}

// parseLayout parses a layout spec into ordered columns. Each non-blank,
// non-comment line is "name,start,length" with a 1-based start. Blank lines and
// lines beginning with '#' are ignored. The error names the offending line so a
// typo is easy to find.
func parseLayout(spec string) ([]fixedField, error) {
	var fields []fixedField
	for i, raw := range strings.Split(spec, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("layout line %d: expected name,start,length", i+1)
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			return nil, fmt.Errorf("layout line %d: empty column name", i+1)
		}
		start, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || start < 1 {
			return nil, fmt.Errorf("layout line %d: start must be a positive integer (1-based)", i+1)
		}
		length, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || length < 1 {
			return nil, fmt.Errorf("layout line %d: length must be a positive integer", i+1)
		}
		fields = append(fields, fixedField{name: name, start: start, length: length})
	}
	if len(fields) == 0 {
		return nil, errors.New("no columns found in layout file (use name,start,length per line)")
	}
	return fields, nil
}

// buildFixedRecords slices each line of content into columns per the layout.
// The result is table-shaped records — a header row followed by one row per
// data line — with a leading marker column that holds fixedMarkerCol on any row
// whose byte length does not match the layout's total width (short or long
// lines), so malformed rows are visible rather than silently mis-aligned. It
// also returns the count of such rows. A UTF-8 BOM at the start of content is
// stripped so byte offsets are not shifted by one.
func buildFixedRecords(content string, fields []fixedField) (recs [][]string, badRows int) {
	content = strings.TrimPrefix(content, "\ufeff")

	header := make([]string, 0, len(fields)+1)
	header = append(header, fixedMarkerCol)
	for _, f := range fields {
		header = append(header, f.name)
	}
	recs = append(recs, header)

	total := 0
	for _, f := range fields {
		if w := f.width(); w > total {
			total = w
		}
	}

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue // skip blank lines (commonly a trailing newline)
		}
		b := []byte(line)
		row := make([]string, 0, len(fields)+1)
		marker := ""
		if len(b) != total {
			marker = fixedMarkerCol
			badRows++
		}
		row = append(row, marker)
		for _, f := range fields {
			s := f.start - 1
			e := s + f.length
			val := ""
			if s < len(b) {
				if e > len(b) {
					e = len(b)
				}
				val = strings.TrimRight(string(b[s:e]), " ")
			}
			row = append(row, val)
		}
		recs = append(recs, row)
	}
	return recs, badRows
}

// startLayoutPrompt opens the local-layout-file prompt over the current preview.
// The last-used path is pre-filled so re-applying after a tweak is quick.
func (m *Model) startLayoutPrompt() {
	ti := textinput.New()
	ti.Prompt = "layout file: "
	ti.Placeholder = "/path/to/layout.txt  (name,start,length per line)"
	ti.CharLimit = 4096
	ti.Width = 48
	if m.lastLayoutPath != "" {
		ti.SetValue(m.lastLayoutPath)
		ti.CursorEnd()
	}
	ti.Focus()
	m.layoutInput = ti
	m.enteringLayout = true
	m.layoutErr = ""
}

// applyLayoutPrompt reads and parses the named layout file, combines it with the
// previewed object's content, and switches to the full-screen table view. On any
// error the prompt stays open with an explanation.
func (m *Model) applyLayoutPrompt() {
	path := strings.TrimSpace(m.layoutInput.Value())
	if path == "" {
		m.layoutErr = "enter a path to a layout file"
		return
	}
	spec, err := readLayoutFile(path)
	if err != nil {
		m.layoutErr = err.Error()
		return
	}
	fields, err := parseLayout(spec)
	if err != nil {
		m.layoutErr = err.Error()
		return
	}
	if strings.TrimSpace(m.previewContent) == "" {
		m.layoutErr = "no object content to apply the layout to"
		return
	}
	recs, bad := buildFixedRecords(m.previewContent, fields)
	if len(recs) < 2 {
		m.layoutErr = "the layout produced no data rows"
		return
	}
	m.lastLayoutPath = path
	m.enteringLayout = false
	m.layoutErr = ""
	m.initFixed(recs, bad)
}

// initFixed loads fixed-width records into the shared table. Mirrors initParquet
// so the existing table, record view, row-window and copy-as-Markdown machinery
// all work unchanged; the schema is fixed (no delimiter/header controls).
func (m *Model) initFixed(recs [][]string, badRows int) {
	m.previewIsParquet = false
	m.previewIsFixed = true
	m.csvAll = recs
	m.csvHeaderRow = 1 // the synthesised column-name row is the header
	m.csvDelim = ','   // unused for fixed-width, kept for a stable info line
	m.csvRecordActive = false
	m.fixedBadRows = badRows
	if m.csvRowCap == 0 && !m.csvRowCapSet {
		m.csvRowCap = defaultCSVRowCap
		m.csvRowCapSet = true
	}
	m.showPreview = false
	m.showCSV = true
	m.buildCSVTable()
}

// fixedInfoLine summarises the column/row windows and the malformed-row count
// for a fixed-width preview. The leading marker column is excluded from the
// reported column count.
func (m *Model) fixedInfoLine(cols int) string {
	dataCols := cols - 1
	if dataCols < 0 {
		dataCols = 0
	}
	colsPart := fmt.Sprintf("%d columns", dataCols)
	if hl, hr := m.csvTable.ColScrollInfo(); hl+hr > 0 {
		header, _ := m.headerAndData()
		// The leading "!" marker column is not a data column: report dataCols as
		// the total and shift the visible range down by 1 so the numbers match
		// the header's "(1) (2) …" line.
		colsPart = m.colWindowInfo(header, dataCols, 1)
	}
	rowsPart := fmt.Sprintf("%d rows", m.csvTotal)
	if m.csvHidden > 0 {
		rowsPart = fmt.Sprintf("first %d + last %d of %d rows (%d hidden)",
			m.csvRowCap, m.csvRowCap, m.csvTotal, m.csvHidden)
	}
	badPart := "no malformed rows"
	if m.fixedBadRows > 0 {
		badPart = fmt.Sprintf("%d malformed rows (marked %s)", m.fixedBadRows, fixedMarkerCol)
	}
	return fmt.Sprintf("fixed-width   ·   %s   ·   %s   ·   %s", colsPart, rowsPart, badPart)
}
