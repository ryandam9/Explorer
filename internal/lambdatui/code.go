package lambdatui

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

// Browsing a function's source downloads its deployment package over the
// network (a presigned S3 GET from GetFunction's Code.Location) — outside the
// AWS SDK — so it is opt-in behind an explicit confirmation and bounded by the
// caps below to stay responsive and zip-bomb-safe.
const (
	maxCodePackageBytes = 50 << 20  // refuse to download a package larger than this
	maxCodeUnzipBytes   = 200 << 20 // total uncompressed bytes read (zip-bomb guard)
	maxCodeFiles        = 2000      // entries listed
	maxCodeFileBytes    = 1 << 20   // bytes kept per file for viewing (1 MiB)
)

// codeFile is one entry from an unzipped deployment package. Data holds up to
// maxCodeFileBytes (Truncated marks a longer file); Binary marks content that
// isn't valid UTF-8 text so the viewer shows a placeholder instead of garbage.
type codeFile struct {
	Name      string
	Size      int64 // declared uncompressed size from the zip header
	Data      []byte
	Truncated bool
	Binary    bool
}

// downloadCode fetches the deployment package from a presigned URL and unzips it
// in memory. The download is capped at maxCodePackageBytes; a larger package is
// refused rather than streamed unbounded.
func downloadCode(ctx context.Context, url string) ([]codeFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Read one byte past the cap so an over-large package is detected rather than
	// silently truncated into a corrupt zip.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCodePackageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCodePackageBytes {
		return nil, fmt.Errorf("deployment package is larger than %s — not downloaded", formatCodeSize(maxCodePackageBytes))
	}
	return unzipCode(data)
}

// unzipCode parses a zip archive into codeFiles, applying the file-count,
// total-size and per-file caps. Pure (no network), so it is unit-tested.
func unzipCode(data []byte) ([]codeFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip package: %w", err)
	}

	var (
		files []codeFile
		total int64
	)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if len(files) >= maxCodeFiles || total >= maxCodeUnzipBytes {
			break
		}
		cf := codeFile{Name: f.Name, Size: int64(f.UncompressedSize64)}
		if data, truncated, err := readZipEntry(f); err == nil {
			cf.Data = data
			cf.Truncated = truncated
			cf.Binary = isBinary(data)
			total += int64(len(data))
		} else {
			cf.Binary = true // unreadable entry: show a placeholder, don't drop it
		}
		files = append(files, cf)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the package contains no files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// readZipEntry reads up to maxCodeFileBytes from one zip entry, reporting whether
// the entry is longer than what was read.
func readZipEntry(f *zip.File) (data []byte, truncated bool, err error) {
	rc, err := f.Open()
	if err != nil {
		return nil, false, err
	}
	defer rc.Close()
	// Read one extra byte to detect truncation.
	b, err := io.ReadAll(io.LimitReader(rc, maxCodeFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > maxCodeFileBytes {
		return b[:maxCodeFileBytes], true, nil
	}
	return b, false, nil
}

// isBinary reports whether b looks like binary rather than displayable text: a
// NUL byte or invalid UTF-8 marks it binary.
func isBinary(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return true
	}
	return !utf8.Valid(b)
}

// codeFileContent renders one file for the viewer: the decoded text, or a
// placeholder for binary/empty entries, with a truncation note when clipped.
func codeFileContent(f codeFile) string {
	if f.Binary {
		return fmt.Sprintf("(binary file — %s, not shown)", formatCodeSize(f.Size))
	}
	if len(f.Data) == 0 {
		return "(empty file)"
	}
	s := sanitizeCode(string(f.Data))
	if f.Truncated {
		s += fmt.Sprintf("\n\n… truncated — showing the first %s of %s", formatCodeSize(maxCodeFileBytes), formatCodeSize(f.Size))
	}
	return s
}

// sanitizeCode strips terminal control bytes (ANSI escapes, stray C0/DEL) that
// would otherwise move the cursor and corrupt the viewer, keeping tab/newline.
func sanitizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// codeLangLabel is a short language tag from a file's extension, for the list.
func codeLangLabel(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 || i == len(name)-1 {
		return "—"
	}
	return strings.ToLower(name[i+1:])
}
