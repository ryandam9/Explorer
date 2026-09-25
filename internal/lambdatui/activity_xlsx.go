package lambdatui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Excel export of an activity run (X in the activity view). The workbook has
// two sheets: the matched events, one row each with the message in full, and
// "Query", which records what produced them (function, day, regex, filter, the
// invocation counts and whether the scan was complete or cut short), so the
// file still makes sense once it is detached from the screen.
//
// Styling: gridlines off and a white background, a coloured header row, and
// thin borders only around the cells that hold data.
const (
	xlsxHeaderFill = "#1F4E79" // deep blue header band
	xlsxHeaderFont = "#FFFFFF"
	xlsxLabelFill  = "#DDEBF7" // pale blue for the Query sheet's field names
	xlsxBorder     = "#BFBFBF" // light grey cell borders
	xlsxWhite      = "#FFFFFF"

	xlsxQuerySheet = "Query"

	// xlsxCellMax is Excel's hard limit on characters in one cell.
	xlsxCellMax = 32767
	// xlsxMessageWidth / xlsxColMax bound column widths (in characters); the
	// message column wraps rather than growing past its width.
	xlsxMessageWidth = 100
	xlsxColMax       = 60
	// xlsxRowMaxPt is Excel's maximum row height, in points.
	xlsxRowMaxPt = 409
)

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// activityWorkbookName names the export after what it holds: the function, the
// day and whether it is a search or a full listing, plus the export time so a
// re-export never overwrites an earlier file.
func activityWorkbookName(q ActivityQuery, now time.Time) string {
	kind := "matches"
	if matchesAll(q.Pattern) {
		kind = "events"
	}
	fn := strings.Trim(unsafeFileChars.ReplaceAllString(q.Function, "-"), "-")
	return fmt.Sprintf("lambda-%s-%s-%s-%s.xlsx", fn, q.Day.Date, kind, now.Format("20060102-150405"))
}

// activitySheetName is the data sheet's name, e.g. "Matches 2026-09-24" (well
// inside Excel's 31-character limit, and free of the characters it forbids).
func activitySheetName(q ActivityQuery) string {
	if matchesAll(q.Pattern) {
		return "Events " + q.Day.Date
	}
	return "Matches " + q.Day.Date
}

// xlsxStyles holds the workbook's style IDs.
type xlsxStyles struct {
	background, header, cell, wrap, time, label int
}

func newXLSXStyles(f *excelize.File) (xlsxStyles, error) {
	white := excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{xlsxWhite}}
	var borders []excelize.Border
	for _, side := range []string{"left", "right", "top", "bottom"} {
		borders = append(borders, excelize.Border{Type: side, Color: xlsxBorder, Style: 1})
	}
	top := &excelize.Alignment{Vertical: "top"}
	timeFmt := "yyyy-mm-dd hh:mm:ss.000"
	defs := []*excelize.Style{
		{Fill: white},
		{
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{xlsxHeaderFill}},
			Font:      &excelize.Font{Bold: true, Color: xlsxHeaderFont},
			Border:    borders,
			Alignment: &excelize.Alignment{Vertical: "center"},
		},
		{Fill: white, Border: borders, Alignment: top},
		{Fill: white, Border: borders, Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}},
		{Fill: white, Border: borders, Alignment: top, CustomNumFmt: &timeFmt},
		{
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{xlsxLabelFill}},
			Font:      &excelize.Font{Bold: true},
			Border:    borders,
			Alignment: top,
		},
	}
	ids := make([]int, len(defs))
	for i, d := range defs {
		id, err := f.NewStyle(d)
		if err != nil {
			return xlsxStyles{}, err
		}
		ids[i] = id
	}
	return xlsxStyles{background: ids[0], header: ids[1], cell: ids[2], wrap: ids[3], time: ids[4], label: ids[5]}, nil
}

// hideGridlines turns off the sheet's gridlines, so only the bordered data
// block has lines.
func hideGridlines(f *excelize.File, sheet string) error {
	off := false
	return f.SetSheetView(sheet, 0, &excelize.ViewOptions{ShowGridLines: &off})
}

// whitenColumns fills every column white — the sheet's background. Call it
// after the column widths (once every column carries a style, each SetColWidth
// re-walks all 16,384 of them, ~0.1s a call) and before writing cells (it
// restyles the cells already in those columns).
func whitenColumns(f *excelize.File, sheet string, st xlsxStyles) error {
	return f.SetColStyle(sheet, "A:XFD", st.background)
}

// xlsxText fits a value into one cell: carriage returns become line breaks
// (Python tracebacks use a bare \r) and text past Excel's per-cell limit is
// cut with a note saying so, rather than failing the whole export.
func xlsxText(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	if utf8.RuneCountInString(s) <= xlsxCellMax {
		return s
	}
	note := fmt.Sprintf(" … [truncated: %d characters, Excel's cell limit is %d]", utf8.RuneCountInString(s), xlsxCellMax)
	r := []rune(s)
	return string(r[:xlsxCellMax-utf8.RuneCountInString(note)]) + note
}

// wallClock re-expresses t's local wall-clock time in UTC, because an Excel
// date has no time zone: the cell should read the time shown on screen (the
// header names the zone).
func wallClock(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// wrappedLines estimates how many lines s takes in a wrapping column of the
// given width, so the row can be made tall enough to show it.
func wrappedLines(s string, width int) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		n += max(1, (utf8.RuneCountInString(line)+width-1)/width)
	}
	return n
}

// WriteActivityWorkbook writes the run's matches (and the Query sheet) to an
// .xlsx file at path.
func WriteActivityWorkbook(path string, q ActivityQuery, stats InvocationStats, statsErr error, scan *LogScan, now time.Time) error {
	if scan == nil {
		return fmt.Errorf("no log scan to export")
	}
	f := excelize.NewFile()
	defer f.Close()
	st, err := newXLSXStyles(f)
	if err != nil {
		return err
	}
	sheet := activitySheetName(q)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	if err := writeMatchesSheet(f, sheet, st, q, scan); err != nil {
		return err
	}
	if _, err := f.NewSheet(xlsxQuerySheet); err != nil {
		return err
	}
	if err := writeQuerySheet(f, st, q, stats, statsErr, scan, now); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	return f.SaveAs(path)
}

func writeMatchesSheet(f *excelize.File, sheet string, st xlsxStyles, q ActivityQuery, scan *LogScan) error {
	if err := hideGridlines(f, sheet); err != nil {
		return err
	}
	loc := q.Day.Start.Location()
	inferred := false
	for _, m := range scan.Matches {
		inferred = inferred || m.RequestIDInferred
	}

	// Columns: time, request ID (+ whether it was inferred), level, one per
	// capture group, the matched text (a search without groups), the full
	// message and the log stream.
	type column struct {
		title string
		style int
		width int // characters; 0 = sized to content
		value func(m Match) any
	}
	cols := []column{
		{title: "Time (" + q.Day.Start.Format("MST") + ")", style: st.time, width: 25,
			value: func(m Match) any { return wallClock(m.Time, loc) }},
		{title: "Request ID", style: st.cell, value: func(m Match) any { return m.RequestID }},
	}
	if inferred {
		cols = append(cols, column{title: "Request ID from START", style: st.cell,
			value: func(m Match) any {
				if m.RequestIDInferred {
					return "yes"
				}
				return ""
			}})
	}
	cols = append(cols, column{title: "Level", style: st.cell, value: func(m Match) any { return m.Level }})
	for i, g := range scan.GroupNames {
		cols = append(cols, column{title: groupHeader(g), style: st.wrap,
			value: func(m Match) any {
				if i < len(m.Groups) {
					return xlsxText(m.Groups[i])
				}
				return ""
			}})
	}
	if scan.MatchColumn() {
		cols = append(cols, column{title: "Matched", style: st.wrap, value: func(m Match) any { return xlsxText(m.Matched) }})
	}
	msgCol := len(cols)
	cols = append(cols,
		column{title: "Message", style: st.wrap, width: xlsxMessageWidth, value: func(m Match) any { return xlsxText(m.Body) }},
		column{title: "Log stream", style: st.cell, value: func(m Match) any { return m.Stream }},
	)

	// Cell values first, so the column widths are known before anything is
	// written: widths, then the white column fill, then the cells and their
	// own styles (a column style applied later would overwrite the cells').
	values := make([][]any, len(scan.Matches))
	widths := make([]int, len(cols))
	for c, col := range cols {
		widths[c] = max(col.width, utf8.RuneCountInString(col.title)+2)
	}
	for r, m := range scan.Matches {
		values[r] = make([]any, len(cols))
		for c, col := range cols {
			v := col.value(m)
			values[r][c] = v
			if s, ok := v.(string); ok && col.width == 0 {
				for _, line := range strings.Split(s, "\n") {
					widths[c] = max(widths[c], utf8.RuneCountInString(line)+2)
				}
			}
		}
	}
	for c := range cols {
		name, _ := excelize.ColumnNumberToName(c + 1)
		w := min(widths[c], xlsxColMax)
		if c == msgCol {
			w = xlsxMessageWidth
		}
		if err := f.SetColWidth(sheet, name, name, float64(w)); err != nil {
			return err
		}
	}
	if err := whitenColumns(f, sheet, st); err != nil {
		return err
	}

	// Header row.
	for c, col := range cols {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		if err := f.SetCellValue(sheet, cell, col.title); err != nil {
			return err
		}
	}
	lastCol, _ := excelize.ColumnNumberToName(len(cols))
	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", st.header); err != nil {
		return err
	}
	if err := f.SetRowHeight(sheet, 1, 20); err != nil {
		return err
	}

	// Data rows. Strings are written as text, never as formulas, so a log line
	// starting with "=" can't execute when the sheet is opened.
	for r, m := range scan.Matches {
		row := r + 2
		for c := range cols {
			cell, _ := excelize.CoordinatesToCellName(c+1, row)
			if err := f.SetCellValue(sheet, cell, values[r][c]); err != nil {
				return err
			}
		}
		lines := wrappedLines(xlsxText(m.Body), xlsxMessageWidth)
		if err := f.SetRowHeight(sheet, row, min(float64(lines)*15, xlsxRowMaxPt)); err != nil {
			return err
		}
	}
	if len(scan.Matches) > 0 {
		for c, col := range cols {
			name, _ := excelize.ColumnNumberToName(c + 1)
			if err := f.SetCellStyle(sheet, name+"2", fmt.Sprintf("%s%d", name, len(scan.Matches)+1), col.style); err != nil {
				return err
			}
		}
	}

	// Keep the header in view and make every column filterable.
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	return f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", lastCol, len(scan.Matches)+1), nil)
}

func writeQuerySheet(f *excelize.File, st xlsxStyles, q ActivityQuery, stats InvocationStats, statsErr error, scan *LogScan, now time.Time) error {
	sheet := xlsxQuerySheet
	if err := hideGridlines(f, sheet); err != nil {
		return err
	}
	regex := "(none — every event of the day)"
	if !matchesAll(q.Pattern) {
		regex = q.Pattern.String()
	}
	filter := q.Filter
	if filter == "" {
		filter = "(none)"
	}
	invocations := stats.InvocationSummary()
	if statsErr != nil {
		invocations = "unavailable: " + statsErr.Error()
	}
	status := "complete — the whole day was read"
	if note := scan.ScanNote(); note != "" {
		status = note
	}
	day := q.Day.Label()
	if q.Day.InProgress(now) {
		day += " — day in progress, counts so far"
	}
	rows := [][2]string{
		{"Function", q.Function},
		{"Region", q.Region},
		{"Log group", q.LogGroup},
		{"Day", day},
		{"Regex", regex},
		{"Server filter", filter},
		{"Invocations", invocations},
		{"Log scan", scan.ScanSummary()},
		{"Rows exported", fmt.Sprintf("%d", len(scan.Matches))},
		{"Scan status", status},
		{"Exported at", now.Format("2006-01-02 15:04:05 MST")},
	}
	if err := f.SetColWidth(sheet, "A", "A", 18); err != nil {
		return err
	}
	if err := f.SetColWidth(sheet, "B", "B", xlsxMessageWidth); err != nil {
		return err
	}
	if err := whitenColumns(f, sheet, st); err != nil {
		return err
	}
	if err := f.SetSheetRow(sheet, "A1", &[]string{"Field", "Value"}); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "B1", st.header); err != nil {
		return err
	}
	if err := f.SetRowHeight(sheet, 1, 20); err != nil {
		return err
	}
	for i, r := range rows {
		row := i + 2
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", row), &[]string{r[0], xlsxText(r[1])}); err != nil {
			return err
		}
	}
	last := len(rows) + 1
	if err := f.SetCellStyle(sheet, "A2", fmt.Sprintf("A%d", last), st.label); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "B2", fmt.Sprintf("B%d", last), st.wrap); err != nil {
		return err
	}
	return nil
}
