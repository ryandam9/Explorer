package s3tui

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/parquet-go/parquet-go"
)

const (
	// parquetDefaultRows is the row window fetched for a preview when the user
	// has not asked for a specific count. The issue (first 1000 rows are enough)
	// drives the default; the "n" key lets the user request a different number.
	parquetDefaultRows = 1000
	// parquetMaxRowsLimit caps how many rows a user can request, since every
	// fetched row is held in memory.
	parquetMaxRowsLimit = 100_000
	// parquetMaxBytes bounds the total bytes a ranged read may pull for one
	// preview, so a malformed footer pointing at huge offsets can't run away.
	// Parquet's footer lets the reader fetch only the row groups needed for the
	// first N rows, so this is a generous safety net, not the typical transfer.
	parquetMaxBytes = 256 << 20
)

// looksLikeParquet reports whether a key's extension marks it as a Parquet file.
func looksLikeParquet(key string) bool {
	switch strings.ToLower(path.Ext(key)) {
	case ".parquet", ".pq", ".parq":
		return true
	}
	return false
}

// parquetRecords reads up to maxRows rows from a Parquet file exposed as an
// io.ReaderAt of the given size. It returns the column names (header), the rows
// as strings, and the file's total row count so the UI can show "first N of M".
//
// It is a pure function over the bytes the reader returns, so it is unit-tested
// against in-memory fixtures. Parquet keeps its schema and row-group offsets in
// a footer at the end of the file, so the reader must be able to seek to the
// end — a truncated prefix cannot be parsed (surfaced as an error by the caller).
func parquetRecords(r io.ReaderAt, size int64, maxRows int) (header []string, data [][]string, total int64, err error) {
	if maxRows <= 0 {
		maxRows = parquetDefaultRows
	}
	// Skip the page index and bloom filters: a preview only reads the leading
	// rows, so loading those side structures would add round-trips for nothing.
	f, err := parquet.OpenFile(r, size, parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return nil, nil, 0, err
	}

	total = f.NumRows()
	cols := f.Schema().Columns()
	header = make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.Join(c, ".") // dotted path for nested leaves
	}

	data = make([][]string, 0, maxRows)
	buf := make([]parquet.Row, 64)
	for _, rg := range f.RowGroups() {
		if len(data) >= maxRows {
			break
		}
		rows := rg.Rows()
		for len(data) < maxRows {
			n, rerr := rows.ReadRows(buf)
			for i := 0; i < n && len(data) < maxRows; i++ {
				data = append(data, parquetRow(buf[i], len(header)))
			}
			if rerr != nil {
				// io.EOF marks the end of the group; any other read error stops
				// this group too — best-effort, the rows already gathered stand.
				break
			}
		}
		rows.Close()
	}
	return header, data, total, nil
}

// parquetRow flattens one Parquet row into n string cells indexed by leaf
// column. A null value renders as empty. Repeated columns (a list leaf) share a
// column index; the last value wins, which is acceptable for a flat preview.
func parquetRow(row parquet.Row, n int) []string {
	rec := make([]string, n)
	for _, v := range row {
		c := v.Column()
		if c < 0 || c >= n {
			continue
		}
		if v.IsNull() {
			rec[c] = ""
		} else {
			rec[c] = v.String()
		}
	}
	return rec
}

// GetObjectParquetPreview reads up to maxRows rows from a Parquet object for the
// table view. It heads the object for its size, then reads through a ranged
// io.ReaderAt so only the footer and the row groups holding the leading rows are
// transferred — a multi-gigabyte file is previewed without downloading it whole.
func (c *S3Client) GetObjectParquetPreview(bucket, key string, maxRows int) (header []string, rows [][]string, total int64, err error) {
	size, err := c.objectSize(bucket, key)
	if err != nil {
		return nil, nil, 0, err
	}
	if size <= 0 {
		return nil, nil, 0, fmt.Errorf("object is empty")
	}
	r := &s3ReaderAt{client: c, bucket: bucket, key: key, size: size, maxBytes: parquetMaxBytes}
	return parquetRecords(r, size, maxRows)
}

// objectSize returns an object's byte length via HeadObject.
func (c *S3Client) objectSize(bucket, key string) (int64, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	head, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(head.ContentLength), nil
}

// s3ReaderAt implements io.ReaderAt over an S3 object using ranged GetObject
// calls, letting the Parquet reader pull only the bytes it needs (footer + the
// leading row groups). The total bytes transferred are bounded by maxBytes.
type s3ReaderAt struct {
	client   *S3Client
	bucket   string
	key      string
	size     int64
	maxBytes int64

	mu      sync.Mutex
	fetched int64
}

func (r *s3ReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 || off >= r.size {
		return 0, io.EOF
	}
	end := off + int64(len(p))
	if end > r.size {
		end = r.size
	}

	r.mu.Lock()
	r.fetched += end - off
	over := r.fetched > r.maxBytes
	r.mu.Unlock()
	if over {
		return 0, fmt.Errorf("parquet preview exceeded the %d-byte read budget", r.maxBytes)
	}

	ctx, cancel := context.WithTimeout(r.client.ctx, time.Minute)
	defer cancel()
	out, err := r.client.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", off, end-1)),
	})
	if err != nil {
		return 0, err
	}
	defer out.Body.Close()

	n, err := io.ReadFull(out.Body, p[:end-off])
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	// io.ReaderAt requires a non-nil error whenever fewer than len(p) bytes are
	// returned; at the tail of the object that reason is EOF.
	if n < len(p) && err == nil {
		err = io.EOF
	}
	return n, err
}
