package s3tui

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
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
		header[i] = safeText(strings.Join(c, ".")) // dotted path for nested leaves
	}
	// Build one value→string formatter per leaf column so logical types
	// (dates, timestamps, decimals, UUIDs) render meaningfully and binary
	// values can't spill raw bytes into the table.
	formatters := parquetFormatters(f, len(header))

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
				data = append(data, parquetRow(buf[i], formatters))
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

// parquetRow flattens one Parquet row into a string cell per leaf column,
// formatting each value with its column's formatter. A null value renders as
// empty. A repeated column (a list/array leaf) emits several values that share
// one column index; they are joined with ", " so list elements are not silently
// dropped. Nulls/empties are skipped, so empty and null both render as blank.
func parquetRow(row parquet.Row, formatters []func(parquet.Value) string) []string {
	rec := make([]string, len(formatters))
	for _, v := range row {
		c := v.Column()
		if c < 0 || c >= len(formatters) {
			continue
		}
		s := formatters[c](v)
		if s == "" {
			continue
		}
		if rec[c] == "" {
			rec[c] = s
		} else {
			rec[c] += ", " + s
		}
	}
	return rec
}

// leafColumns returns the file's leaf columns (those that hold values), in
// column-index order, by walking the schema tree depth-first.
func leafColumns(root *parquet.Column) []*parquet.Column {
	var leaves []*parquet.Column
	var walk func(c *parquet.Column)
	walk = func(c *parquet.Column) {
		if c.Leaf() {
			leaves = append(leaves, c)
			return
		}
		for _, child := range c.Columns() {
			walk(child)
		}
	}
	walk(root)
	return leaves
}

// parquetFormatters returns one value→string formatter per leaf column, indexed
// by the column's position in a row. Columns with no resolvable type fall back
// to the physical-type formatter.
func parquetFormatters(f *parquet.File, n int) []func(parquet.Value) string {
	fns := make([]func(parquet.Value) string, n)
	for _, c := range leafColumns(f.Root()) {
		idx := c.Index()
		if idx < 0 || idx >= n {
			continue
		}
		fns[idx] = columnFormatter(c.Type())
	}
	for i := range fns {
		if fns[i] == nil {
			fns[i] = formatParquetValue
		}
	}
	return fns
}

// columnFormatter picks a value formatter from a column's logical type, falling
// back to the physical-type formatter when there is no (or no special) logical
// type. Every formatter renders a null as the empty string.
func columnFormatter(t parquet.Type) func(parquet.Value) string {
	lt := t.LogicalType()
	if lt == nil {
		return formatParquetValue
	}
	switch {
	case lt.Date != nil:
		return nullable(func(v parquet.Value) string {
			return time.Unix(0, 0).UTC().AddDate(0, 0, int(v.Int32())).Format("2006-01-02")
		})
	case lt.Timestamp != nil:
		unit := lt.Timestamp.Unit
		return nullable(func(v parquet.Value) string {
			return formatParquetTimestamp(v.Int64(), unit)
		})
	case lt.Time != nil:
		unit := lt.Time.Unit
		return nullable(func(v parquet.Value) string {
			return formatParquetTime(valueInt(v), unit)
		})
	case lt.Decimal != nil:
		scale := int(lt.Decimal.Scale)
		return nullable(func(v parquet.Value) string {
			return formatParquetDecimal(v, scale)
		})
	case lt.UUID != nil:
		return nullable(func(v parquet.Value) string {
			return formatUUID(v.ByteArray())
		})
	case lt.UTF8 != nil, lt.Json != nil, lt.Bson != nil, lt.Enum != nil:
		return nullable(func(v parquet.Value) string {
			return safeText(string(v.ByteArray()))
		})
	case lt.Integer != nil && !lt.Integer.IsSigned:
		return nullable(func(v parquet.Value) string {
			return strconv.FormatUint(v.Uint64(), 10)
		})
	}
	return formatParquetValue
}

// nullable wraps a formatter so a null value always renders as the empty string.
func nullable(f func(parquet.Value) string) func(parquet.Value) string {
	return func(v parquet.Value) string {
		if v.IsNull() {
			return ""
		}
		return f(v)
	}
}

// formatParquetValue formats a value from its physical type. Byte arrays are
// shown as text when they decode as printable UTF-8, otherwise as a hex dump so
// binary (UUIDs, blobs, raw decimals) never spills raw bytes into the table.
func formatParquetValue(v parquet.Value) string {
	if v.IsNull() {
		return ""
	}
	switch v.Kind() {
	case parquet.ByteArray, parquet.FixedLenByteArray:
		return safeBytes(v.ByteArray())
	case parquet.Double:
		return strconv.FormatFloat(v.Double(), 'g', -1, 64)
	case parquet.Float:
		return strconv.FormatFloat(float64(v.Float()), 'g', -1, 32)
	default:
		return v.String()
	}
}

// valueInt returns a value's integer payload regardless of its 32/64-bit
// physical width (TIME is stored as INT32 for millis, INT64 otherwise).
func valueInt(v parquet.Value) int64 {
	if v.Kind() == parquet.Int32 {
		return int64(v.Int32())
	}
	return v.Int64()
}

// formatParquetTimestamp renders an epoch offset (in the column's time unit) as
// a UTC date-time.
func formatParquetTimestamp(n int64, unit format.TimeUnit) string {
	var t time.Time
	switch {
	case unit.Micros != nil:
		t = time.UnixMicro(n)
	case unit.Nanos != nil:
		t = time.Unix(0, n)
	default: // millis (the default/most common)
		t = time.UnixMilli(n)
	}
	return t.UTC().Format("2006-01-02 15:04:05.999999999")
}

// formatParquetTime renders a time-of-day offset (in the column's time unit) as
// HH:MM:SS[.fraction].
func formatParquetTime(n int64, unit format.TimeUnit) string {
	var d time.Duration
	switch {
	case unit.Micros != nil:
		d = time.Duration(n) * time.Microsecond
	case unit.Nanos != nil:
		d = time.Duration(n)
	default:
		d = time.Duration(n) * time.Millisecond
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	frac := d - s*time.Second
	out := fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	if frac > 0 {
		out += strings.TrimRight(fmt.Sprintf(".%09d", frac), "0")
	}
	return out
}

// formatParquetDecimal renders a DECIMAL value (stored as int32/int64 or as a
// big-endian two's-complement byte array) using its scale.
func formatParquetDecimal(v parquet.Value, scale int) string {
	var unscaled *big.Int
	switch v.Kind() {
	case parquet.Int32:
		unscaled = big.NewInt(int64(v.Int32()))
	case parquet.Int64:
		unscaled = big.NewInt(v.Int64())
	case parquet.ByteArray, parquet.FixedLenByteArray:
		unscaled = bigIntFromTwosComplement(v.ByteArray())
	default:
		return v.String()
	}
	return scaleDecimal(unscaled, scale)
}

// bigIntFromTwosComplement decodes big-endian two's-complement bytes (the
// Parquet DECIMAL byte encoding) into a signed big.Int.
func bigIntFromTwosComplement(b []byte) *big.Int {
	n := new(big.Int).SetBytes(b)
	if len(b) > 0 && b[0]&0x80 != 0 { // negative: subtract 2^(8*len)
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(8*len(b))))
	}
	return n
}

// scaleDecimal places the decimal point `scale` digits from the right of an
// unscaled integer, e.g. (123456, 2) → "1234.56".
func scaleDecimal(unscaled *big.Int, scale int) string {
	if scale <= 0 {
		return unscaled.String()
	}
	neg := unscaled.Sign() < 0
	digits := new(big.Int).Abs(unscaled).String()
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	point := len(digits) - scale
	out := digits[:point] + "." + digits[point:]
	if neg {
		out = "-" + out
	}
	return out
}

// formatUUID renders 16 raw UUID bytes as the canonical 8-4-4-4-12 form,
// falling back to a hex dump for an unexpected length.
func formatUUID(b []byte) string {
	if len(b) != 16 {
		return safeBytes(b)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// safeBytes renders a byte array as text when it is printable UTF-8, otherwise
// as a capped hex dump, so non-text binary (UUIDs, blobs, raw decimals) never
// injects control bytes into the terminal. Bytes that are valid UTF-8 but carry
// non-whitespace control characters are treated as binary, since a real text
// column would not contain them.
func safeBytes(b []byte) string {
	if utf8.Valid(b) && printableUTF8(string(b)) {
		return safeText(string(b))
	}
	const max = 32
	if len(b) > max {
		return "0x" + hex.EncodeToString(b[:max]) + "…"
	}
	return "0x" + hex.EncodeToString(b)
}

// printableUTF8 reports whether s contains only printable runes and common
// whitespace (tab/newline/carriage-return) — i.e. it reads as text rather than
// binary that merely happens to decode as UTF-8.
func printableUTF8(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if unsafeRune(r) {
			return false
		}
	}
	return true
}

// safeText replaces control / non-printable runes with spaces so previewed text
// (including UTF-8 byte arrays) cannot move the cursor or inject ANSI escapes.
// The shared table widget still trims and length-caps the result.
func safeText(s string) string {
	if !strings.ContainsFunc(s, unsafeRune) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return ' '
		}
		return r
	}, s)
}

// unsafeRune reports whether r is a C0/C1 control character, DEL, or the UTF-8
// replacement rune — none of which are safe to emit verbatim into the table.
func unsafeRune(r rune) bool {
	return r == utf8.RuneError || r < 0x20 || (r >= 0x7f && r < 0xa0)
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
