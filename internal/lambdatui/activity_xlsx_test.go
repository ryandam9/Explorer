package lambdatui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/xuri/excelize/v2"

	"github.com/ryandam9/aws_explorer/internal/downloads"
)

func xlsxFixture(t *testing.T, pattern string, maxMatches int) (ActivityQuery, *LogScan) {
	t.Helper()
	syd, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	day, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, syd), syd)
	re := regexp.MustCompile(pattern)
	q := ActivityQuery{Region: "ap-southeast-2", Function: "stocks-notify", LogGroup: "/aws/lambda/stocks-notify",
		Day: day, Pattern: re, MaxMatches: maxMatches}
	scan := NewLogScan(re, "", maxMatches)
	at := day.Start.Add(7*time.Hour + 35*time.Minute) // 07:35 local
	scan.Ingest([]LogEvent{
		{Time: at, Stream: "s1", Message: "START RequestId: 3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c Version: $LATEST"},
		{Time: at.Add(time.Second), Stream: "s1", Message: "[INFO]\t2026-09-23T21:35:37.940Z\t3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c\tSent: stocks: asx.db is ready (2.33 MB)"},
		{Time: at.Add(2 * time.Second), Stream: "s1", Message: "=HYPERLINK(\"http://x\") Sent: stocks: bad.db is ready (0 MB)"},
		{Time: at.Add(3 * time.Second), Stream: "s1", Message: "[ERROR] RuntimeError: boom\rTraceback (most recent call last):\r  File \"app.py\", line 9 Sent: x.db is ready (1 MB)"},
	})
	scan.Advance(false)
	return q, scan
}

func TestWriteActivityWorkbook(t *testing.T) {
	q, scan := xlsxFixture(t, `Sent: (?:\S+ )?(?P<db>\w+)\.db is ready \((?P<size>[\d.]+ \w+)\)`, 1000)
	if len(scan.Matches) != 3 {
		t.Fatalf("fixture should match 3 lines, got %d", len(scan.Matches))
	}
	now := time.Date(2026, 9, 25, 14, 30, 12, 0, q.Day.Start.Location())
	path := filepath.Join(t.TempDir(), activityWorkbookName(q, now))
	if got := filepath.Base(path); got != "lambda-stocks-notify-2026-09-24-matches-20260925-143012.xlsx" {
		t.Errorf("workbook name = %q", got)
	}
	if err := WriteActivityWorkbook(path, q, InvocationStats{HasData: true, Invocations: 3}, nil, scan, now); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := f.GetSheetList(); len(got) != 2 || got[0] != "Matches 2026-09-24" || got[1] != "Query" {
		t.Fatalf("sheets = %q", got)
	}
	sheet := "Matches 2026-09-24"

	rows, _ := f.GetRows(sheet)
	wantHeader := []string{"Time (AEST)", "Request ID", "Request ID from START", "Level", "db", "size", "Message", "Log stream"}
	if strings.Join(rows[0], "|") != strings.Join(wantHeader, "|") {
		t.Errorf("header = %q, want %q", rows[0], wantHeader)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want header + 3", len(rows))
	}
	if rows[1][1] != "3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c" || rows[1][4] != "asx" || rows[1][5] != "2.33 MB" {
		t.Errorf("first match row = %q", rows[1])
	}
	if rows[2][2] != "yes" {
		t.Errorf("an ID taken from the START line should be flagged: %q", rows[2])
	}

	// The time is a real date-time (a number with a date format), in local time.
	if typ, _ := f.GetCellType(sheet, "A2"); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Errorf("time should be a date value, got type %v", typ)
	}
	if v, _ := f.GetCellValue(sheet, "A2"); !strings.HasPrefix(v, "2026-09-24 07:35:01") {
		t.Errorf("time cell = %q, want the local wall-clock time", v)
	}

	// A log line that looks like a formula stays text.
	if formula, _ := f.GetCellFormula(sheet, "G3"); formula != "" {
		t.Errorf("log text became a formula: %q", formula)
	}
	if v, _ := f.GetCellValue(sheet, "G3"); !strings.HasPrefix(v, "=HYPERLINK") {
		t.Errorf("formula-looking text should be kept as text: %q", v)
	}
	// Traceback lines (bare \r) become real line breaks in a wrapping cell.
	if v, _ := f.GetCellValue(sheet, "G4"); !strings.Contains(v, "boom\nTraceback") {
		t.Errorf("\\r should become a line break: %q", v)
	}

	// Styling: gridlines off, coloured bold header, bordered data, white elsewhere.
	if opts, _ := f.GetSheetView(sheet, 0); opts.ShowGridLines == nil || *opts.ShowGridLines {
		t.Error("gridlines should be hidden")
	}
	hs := cellStyle(t, f, sheet, "A1")
	if len(hs.Fill.Color) == 0 || !strings.EqualFold("#"+strings.TrimPrefix(hs.Fill.Color[0], "#"), xlsxHeaderFill) || hs.Font == nil || !hs.Font.Bold {
		t.Errorf("header style = fill %v font %+v", hs.Fill, hs.Font)
	}
	if ds := cellStyle(t, f, sheet, "G4"); len(ds.Border) != 4 || ds.Alignment == nil || !ds.Alignment.WrapText {
		t.Errorf("message cells should be bordered and wrapped: %+v", ds)
	}
	if es := cellStyle(t, f, sheet, "J10"); len(es.Border) != 0 || len(es.Fill.Color) == 0 || !strings.EqualFold("#"+strings.TrimPrefix(es.Fill.Color[0], "#"), xlsxWhite) {
		t.Errorf("cells outside the data should be white with no border: %+v", es)
	}

	if w, _ := f.GetColWidth(sheet, "G"); w != xlsxMessageWidth {
		t.Errorf("message column width = %v, want %d (the white fill must not reset widths)", w, xlsxMessageWidth)
	}

	// The Query sheet records what produced the rows.
	qrows, _ := f.GetRows("Query")
	got := map[string]string{}
	for _, r := range qrows[1:] {
		got[r[0]] = r[1]
	}
	if got["Function"] != "stocks-notify" || !strings.Contains(got["Regex"], "(?P<db>") ||
		got["Rows exported"] != "3" || !strings.HasPrefix(got["Scan status"], "complete") {
		t.Errorf("query sheet = %v", got)
	}
}

func cellStyle(t *testing.T, f *excelize.File, sheet, cell string) *excelize.Style {
	t.Helper()
	id, err := f.GetCellStyle(sheet, cell)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.GetStyle(id)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// A capped scan and a full listing are both labelled: the sheet is named for
// events, the MATCH column is absent, and the Query sheet says the scan stopped.
func TestWriteActivityWorkbookAllEventsCapped(t *testing.T) {
	q, scan := xlsxFixture(t, ``, 2)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, q.Day.Start.Location())
	path := filepath.Join(t.TempDir(), activityWorkbookName(q, now))
	if !strings.Contains(path, "-2026-09-24-events-") {
		t.Errorf("an all-events export should say so in its name: %s", path)
	}
	if err := WriteActivityWorkbook(path, q, InvocationStats{}, nil, scan, now); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("Events 2026-09-24")
	for _, h := range rows[0] {
		if h == "Matched" {
			t.Error("an all-events listing has no Matched column")
		}
	}
	qrows, _ := f.GetRows("Query")
	var status string
	for _, r := range qrows {
		if r[0] == "Scan status" {
			status = r[1]
		}
	}
	if !strings.Contains(status, "stopped at 2 events") {
		t.Errorf("the cap must be recorded, got %q", status)
	}
}

func TestXLSXTextLimit(t *testing.T) {
	long := strings.Repeat("a", xlsxCellMax+10)
	got := xlsxText(long)
	if n := len([]rune(got)); n != xlsxCellMax || !strings.Contains(got, "truncated") {
		t.Errorf("an over-long value should be cut to the cell limit with a note: %d chars", n)
	}
}

// X exports from the activity view: refused while the scan runs, then written
// to the downloads directory with the path in the toast.
func TestActivityExportKey(t *testing.T) {
	dir := t.TempDir()
	downloads.Init(dir)
	defer downloads.Init("")

	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{logEvt(1790000000000, "s", "Copied in/a.csv to out/a.csv")}},
	}}
	mm := newActivityTestModel(logs)
	mm.Update(key("a"))
	mm.act.inputs[actFieldPattern].SetValue(`Copied (\S+)`)
	_, cmd := mm.Update(key("enter"))

	mm.Update(key("X"))
	if !strings.Contains(mm.toast, "Scan still running") {
		t.Errorf("X mid-scan should be refused, toast %q", mm.toast)
	}
	runCmd(mm, cmd)

	mm.Update(key("X"))
	files, _ := filepath.Glob(filepath.Join(dir, "lambda-copy-object-*-matches-*.xlsx"))
	if len(files) != 1 {
		t.Fatalf("expected one workbook in %s, got %v (toast %q)", dir, files, mm.toast)
	}
	if !strings.Contains(mm.toast, "Exported 1 rows to "+files[0]) {
		t.Errorf("toast should name the file: %q", mm.toast)
	}
	if st, err := os.Stat(files[0]); err != nil || st.Size() == 0 {
		t.Errorf("workbook not written: %v", err)
	}
}
