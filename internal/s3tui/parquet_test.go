package s3tui

import (
	"bytes"
	"testing"

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
