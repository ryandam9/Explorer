package s3tui

import (
	"bytes"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestLooksLikeParquet(t *testing.T) {
	for _, k := range []string{"data.parquet", "DATA.PARQUET", "part-0000.pq", "x.parq", "a/b/c.parquet"} {
		if !looksLikeParquet(k) {
			t.Errorf("looksLikeParquet(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"data.csv", "notes.txt", "archive.tar.gz", "noext", "parquet"} {
		if looksLikeParquet(k) {
			t.Errorf("looksLikeParquet(%q) = true, want false", k)
		}
	}
}

type parquetTestRow struct {
	ID    int64   `parquet:"id"`
	Name  string  `parquet:"name"`
	Score float64 `parquet:"score"`
	Ok    bool    `parquet:"ok"`
}

// writeParquet builds an in-memory Parquet file from rows.
func writeParquet(t *testing.T, rows []parquetTestRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[parquetTestRow](&buf)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

func TestParquetRecords(t *testing.T) {
	rows := []parquetTestRow{
		{ID: 1, Name: "alice", Score: 1.5, Ok: true},
		{ID: 2, Name: "bob", Score: 2.0, Ok: false},
		{ID: 3, Name: "carol", Score: 3.25, Ok: true},
	}
	data := writeParquet(t, rows)

	header, recs, total, err := parquetRecords(bytes.NewReader(data), int64(len(data)), 1000)
	if err != nil {
		t.Fatalf("parquetRecords: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	wantHeader := []string{"id", "name", "score", "ok"}
	if len(header) != len(wantHeader) {
		t.Fatalf("header = %v, want %v", header, wantHeader)
	}
	for i, h := range wantHeader {
		if header[i] != h {
			t.Errorf("header[%d] = %q, want %q", i, header[i], h)
		}
	}
	if len(recs) != 3 {
		t.Fatalf("got %d rows, want 3", len(recs))
	}
	if recs[0][0] != "1" || recs[0][1] != "alice" || recs[0][3] != "true" {
		t.Errorf("row 0 = %v, want [1 alice 1.5 true]", recs[0])
	}
	if recs[1][3] != "false" {
		t.Errorf("row 1 ok = %q, want false", recs[1][3])
	}
}

func TestParquetRecordsRowCap(t *testing.T) {
	var rows []parquetTestRow
	for i := 0; i < 50; i++ {
		rows = append(rows, parquetTestRow{ID: int64(i), Name: "x"})
	}
	data := writeParquet(t, rows)

	header, recs, total, err := parquetRecords(bytes.NewReader(data), int64(len(data)), 10)
	if err != nil {
		t.Fatalf("parquetRecords: %v", err)
	}
	if total != 50 {
		t.Errorf("total = %d, want 50 (full file count even when capped)", total)
	}
	if len(recs) != 10 {
		t.Errorf("got %d rows, want 10 (capped)", len(recs))
	}
	if len(header) == 0 {
		t.Error("header is empty")
	}
}

func TestParquetRecordsNotParquet(t *testing.T) {
	junk := []byte("this is not a parquet file at all")
	if _, _, _, err := parquetRecords(bytes.NewReader(junk), int64(len(junk)), 100); err == nil {
		t.Error("expected an error for non-parquet bytes, got nil")
	}
}

// logicalRow exercises the logical types that Value.String would otherwise
// render as raw physical values (or raw bytes).
type logicalRow struct {
	Day  int32    `parquet:"day,date"`
	TS   int64    `parquet:"ts,timestamp(millisecond)"`
	Amt  int64    `parquet:"amt,decimal(2:18)"`
	ID   [16]byte `parquet:"id,uuid"`
	Blob []byte   `parquet:"blob"`
}

func TestParquetRecordsLogicalTypes(t *testing.T) {
	id := [16]byte{0xa6, 0x1f, 0x79, 0x6a, 0x4a, 0x96, 0x41, 0x09, 0xb7, 0x1e, 0x2d, 0xab, 0x95, 0xa5, 0x0c, 0x87}
	row := logicalRow{
		Day:  20000, // 20000 days after the unix epoch
		TS:   time.Date(2026, 6, 17, 10, 30, 0, 0, time.UTC).UnixMilli(),
		Amt:  123456,                   // scale 2 → 1234.56
		ID:   id,                       // canonical 8-4-4-4-12
		Blob: []byte{0x00, 0x1b, 0xff}, // non-UTF-8 → hex, no control bytes
	}

	var buf bytes.Buffer
	w := parquet.NewGenericWriter[logicalRow](&buf)
	if _, err := w.Write([]logicalRow{row}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data := buf.Bytes()

	_, recs, _, err := parquetRecords(bytes.NewReader(data), int64(len(data)), 10)
	if err != nil {
		t.Fatalf("parquetRecords: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d rows, want 1", len(recs))
	}
	want := []string{"2024-10-04", "2026-06-17 10:30:00", "1234.56", "a61f796a-4a96-4109-b71e-2dab95a50c87", "0x001bff"}
	for i, w := range want {
		if recs[0][i] != w {
			t.Errorf("cell %d = %q, want %q", i, recs[0][i], w)
		}
	}
}

type listRow struct {
	Name string   `parquet:"name"`
	Tags []string `parquet:"tags"`
}

func TestParquetRecordsRepeatedColumn(t *testing.T) {
	rows := []listRow{
		{Name: "a", Tags: []string{"x", "y", "z"}},
		{Name: "b", Tags: nil},
		{Name: "c", Tags: []string{"only"}},
	}
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[listRow](&buf)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data := buf.Bytes()

	header, recs, _, err := parquetRecords(bytes.NewReader(data), int64(len(data)), 10)
	if err != nil {
		t.Fatalf("parquetRecords: %v", err)
	}
	// Find the tags column by header name (nested leaves use a dotted path).
	tagsCol := -1
	for i, h := range header {
		if strings.HasPrefix(h, "tags") {
			tagsCol = i
		}
	}
	if tagsCol < 0 {
		t.Fatalf("tags column not found in header %v", header)
	}
	if recs[0][tagsCol] != "x, y, z" {
		t.Errorf("row 0 tags = %q, want \"x, y, z\"", recs[0][tagsCol])
	}
	if recs[1][tagsCol] != "" {
		t.Errorf("row 1 tags = %q, want empty", recs[1][tagsCol])
	}
	if recs[2][tagsCol] != "only" {
		t.Errorf("row 2 tags = %q, want \"only\"", recs[2][tagsCol])
	}
}

func TestScaleDecimal(t *testing.T) {
	cases := []struct {
		unscaled int64
		scale    int
		want     string
	}{
		{123456, 2, "1234.56"},
		{-123456, 2, "-1234.56"},
		{5, 3, "0.005"},
		{-5, 3, "-0.005"},
		{100, 0, "100"},
		{0, 2, "0.00"},
	}
	for _, c := range cases {
		if got := scaleDecimal(big.NewInt(c.unscaled), c.scale); got != c.want {
			t.Errorf("scaleDecimal(%d, %d) = %q, want %q", c.unscaled, c.scale, got, c.want)
		}
	}
}

func TestBigIntFromTwosComplement(t *testing.T) {
	if got := bigIntFromTwosComplement([]byte{0x00, 0x7b}); got.Int64() != 123 {
		t.Errorf("positive = %d, want 123", got.Int64())
	}
	if got := bigIntFromTwosComplement([]byte{0xff, 0x85}); got.Int64() != -123 {
		t.Errorf("negative = %d, want -123", got.Int64())
	}
}

func TestSafeBytes(t *testing.T) {
	if got := safeBytes([]byte("hello")); got != "hello" {
		t.Errorf("text = %q, want hello", got)
	}
	if got := safeBytes([]byte{0xff, 0xfe, 0x00}); got != "0xfffe00" {
		t.Errorf("binary = %q, want 0xfffe00", got)
	}
	// Valid UTF-8 that is pure control bytes reads as binary → hex.
	if got := safeBytes([]byte{0x01, 0x02}); got != "0x0102" {
		t.Errorf("control bytes = %q, want 0x0102", got)
	}
	// A long blob is capped with an ellipsis.
	long := make([]byte, 40)
	long[0] = 0xff
	if got := safeBytes(long); !strings.HasSuffix(got, "…") {
		t.Errorf("long blob = %q, want trailing ellipsis", got)
	}
}

func TestSafeText(t *testing.T) {
	if got := safeText("clean text"); got != "clean text" {
		t.Errorf("clean = %q", got)
	}
	// Control bytes embedded in real text are neutralised to spaces in place.
	if got := safeText("a\x1bb\tc"); got != "a b c" {
		t.Errorf("control text = %q, want \"a b c\"", got)
	}
	// Printable Unicode is preserved.
	if got := safeText("café — naïve"); got != "café — naïve" {
		t.Errorf("unicode = %q", got)
	}
}

func TestFormatUUID(t *testing.T) {
	b := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	if got := formatUUID(b); got != "01020304-0506-0708-090a-0b0c0d0e0f10" {
		t.Errorf("formatUUID = %q", got)
	}
	if got := formatUUID([]byte{0x01, 0x02}); got != "0x0102" {
		t.Errorf("short uuid = %q, want hex fallback", got)
	}
}
